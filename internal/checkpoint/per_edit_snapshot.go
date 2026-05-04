package checkpoint

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
)

const (
	perEditPathHashLen     = 16
	perEditMaxCaptureBytes = 64 * 1024 * 1024
	perEditIndexFileName   = "index.jsonl"
)

// ConflictResult 是 RestoreResult.Conflict 字段的占位类型，保留以维持 Gateway/CLI 旧契约。
// per-edit 后端不做冲突检测，HasConflict 始终为 false。
type ConflictResult struct {
	HasConflict bool `json:"has_conflict"`
}

// FileVersionMeta 描述某次 CapturePreWrite 时刻的元信息，伴随 .bin 内容文件落盘。
type FileVersionMeta struct {
	PathHash    string      `json:"path_hash"`
	DisplayPath string      `json:"display_path"`
	Version     int         `json:"version"`
	Existed     bool        `json:"existed"`
	IsDir       bool        `json:"is_dir,omitempty"`
	Mode        os.FileMode `json:"mode,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

// CheckpointMeta 是 cp_<checkpointID>.json 的内容。
type CheckpointMeta struct {
	CheckpointID string         `json:"checkpoint_id"`
	CreatedAt    time.Time      `json:"created_at"`
	FileVersions map[string]int `json:"file_versions"`
}

// perEditIndexEntry 是 index.jsonl 的单行结构，进程重启时用于重建内存索引。
type perEditIndexEntry struct {
	PathHash    string `json:"path_hash"`
	DisplayPath string `json:"display_path"`
	Version     int    `json:"version"`
}

// PerEditSnapshotStore 提供基于"工具触碰"的版本化增量文件历史。
// 每个版本独立寻址（<pathHash>@v<n>.bin/.meta），checkpoint 仅存 (pathHash → version) 映射。
// 同一 workdir 下跨 session 共享 file-history 目录，pathHash 已唯一标识 abs path。
type PerEditSnapshotStore struct {
	fileHistoryDir string
	checkpointsDir string
	workdir        string

	indexMu        sync.Mutex
	pathToVersions map[string][]int
	displayPaths   map[string]string

	pendingMu sync.Mutex
	pending   map[string]int
}

// NewPerEditSnapshotStore 创建文件历史存储实例并从磁盘重建内存索引。
// projectDir 为 ~/.neocode/projects/<workdir_hash>，workdir 为实际工作区根目录。
func NewPerEditSnapshotStore(projectDir, workdir string) *PerEditSnapshotStore {
	store := &PerEditSnapshotStore{
		fileHistoryDir: filepath.Join(projectDir, "file-history"),
		checkpointsDir: filepath.Join(projectDir, "checkpoints"),
		workdir:        workdir,
		pathToVersions: make(map[string][]int),
		displayPaths:   make(map[string]string),
		pending:        make(map[string]int),
	}
	store.loadIndexFromDisk()
	return store
}

// IsAvailable 永远返回 true，纯文件实现没有外部依赖。
func (s *PerEditSnapshotStore) IsAvailable() bool {
	return s != nil
}

// CapturePreWrite 在工具修改 absPath 之前为其创建一个新版本（含旧内容）。
// 同一 path 在同一轮（Reset 之间）内多次调用只保留首次：返回首次分配的版本号。
// 文件不存在时 .meta.Existed=false、.bin 为空文件。
func (s *PerEditSnapshotStore) CapturePreWrite(absPath string) (int, error) {
	cleanPath := filepath.Clean(absPath)
	if cleanPath == "" || cleanPath == "." {
		return 0, fmt.Errorf("per-edit: empty path")
	}
	hash := perEditPathHash(cleanPath)

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	s.pendingMu.Lock()
	if v, ok := s.pending[hash]; ok {
		s.pendingMu.Unlock()
		return v, nil
	}
	s.pendingMu.Unlock()

	versions := s.pathToVersions[hash]
	nextVersion := 1
	if len(versions) > 0 {
		nextVersion = versions[len(versions)-1] + 1
	}

	content, existed, isDir, mode, err := readFileForCapture(cleanPath)
	if err != nil {
		return 0, fmt.Errorf("per-edit: read %s: %w", cleanPath, err)
	}

	meta := FileVersionMeta{
		PathHash:    hash,
		DisplayPath: cleanPath,
		Version:     nextVersion,
		Existed:     existed,
		IsDir:       isDir,
		Mode:        mode,
		CreatedAt:   time.Now().UTC(),
	}

	if err := s.writeVersionFiles(meta, content); err != nil {
		return 0, err
	}
	if err := s.appendIndex(perEditIndexEntry{
		PathHash:    hash,
		DisplayPath: cleanPath,
		Version:     nextVersion,
	}); err != nil {
		return 0, fmt.Errorf("per-edit: append index: %w", err)
	}

	s.pathToVersions[hash] = append(versions, nextVersion)
	s.displayPaths[hash] = cleanPath

	s.pendingMu.Lock()
	s.pending[hash] = nextVersion
	s.pendingMu.Unlock()

	return nextVersion, nil
}

// CaptureBatch 批量调用 CapturePreWrite，返回成功 capture 的 abs path 列表。
// 单条失败立即返回，已 capture 的 path 仍在返回切片中。
func (s *PerEditSnapshotStore) CaptureBatch(absPaths []string) ([]string, error) {
	captured := make([]string, 0, len(absPaths))
	for _, p := range absPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if _, err := s.CapturePreWrite(p); err != nil {
			return captured, err
		}
		captured = append(captured, filepath.Clean(p))
	}
	return captured, nil
}

// CapturePostDelete 为已删除的路径写入 post-delete 版本（Existed=false）。
// 这些版本不进入 pending，而是直接追加到索引，供 restore/diff 的 v_next 查询使用。
func (s *PerEditSnapshotStore) CapturePostDelete(absPaths []string) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	for _, p := range absPaths {
		cleanPath := filepath.Clean(p)
		if cleanPath == "" || cleanPath == "." {
			continue
		}
		hash := perEditPathHash(cleanPath)

		versions := s.pathToVersions[hash]
		nextVersion := 1
		if len(versions) > 0 {
			nextVersion = versions[len(versions)-1] + 1
		}

		meta := FileVersionMeta{
			PathHash:    hash,
			DisplayPath: cleanPath,
			Version:     nextVersion,
			Existed:     false,
			IsDir:       false,
			Mode:        0,
			CreatedAt:   time.Now().UTC(),
		}
		metaPath := s.versionMetaPath(hash, nextVersion)
		if err := s.writeVersionMetaOnly(metaPath, meta); err != nil {
			return fmt.Errorf("per-edit: post-delete %s: %w", cleanPath, err)
		}
		if err := s.appendIndex(perEditIndexEntry{
			PathHash:    hash,
			DisplayPath: cleanPath,
			Version:     nextVersion,
		}); err != nil {
			return fmt.Errorf("per-edit: append post-delete index %s: %w", cleanPath, err)
		}

		s.pathToVersions[hash] = append(versions, nextVersion)
		s.displayPaths[hash] = cleanPath
	}
	return nil
}

// Finalize 把当前 pending 的 (pathHash → version) 映射写入 cp_<checkpointID>.json。
// pending 为空时返回 (false, nil)，不创建空 checkpoint。调用方在 Finalize 后应调用 Reset。
func (s *PerEditSnapshotStore) Finalize(checkpointID string) (bool, error) {
	if checkpointID == "" {
		return false, fmt.Errorf("per-edit: empty checkpointID")
	}
	s.pendingMu.Lock()
	if len(s.pending) == 0 {
		s.pendingMu.Unlock()
		return false, nil
	}
	snapshot := make(map[string]int, len(s.pending))
	for k, v := range s.pending {
		snapshot[k] = v
	}
	s.pendingMu.Unlock()

	meta := CheckpointMeta{
		CheckpointID: checkpointID,
		CreatedAt:    time.Now().UTC(),
		FileVersions: snapshot,
	}
	if err := s.writeCheckpointMeta(meta); err != nil {
		return false, err
	}
	return true, nil
}

// Reset 清空 pending 映射，每轮 turn 开始时调用，避免跨轮残留。
func (s *PerEditSnapshotStore) Reset() {
	s.pendingMu.Lock()
	s.pending = make(map[string]int)
	s.pendingMu.Unlock()
}

// Restore 还原到指定 checkpoint 时刻的工作区文件状态。
// 算法核心（"下一版本即修改后状态"对偶）：
//   - 对每个 (pathHash, v_A)：找 pathToVersions[hash] 中 v_A 之后的下一个版本 v_next。
//   - v_next 存在时把 v_next.bin 写回 displayPath（v_next.meta.Existed=false 时改为删除）；
//     v_next 内容即"checkpoint A 时刻的状态"。
//   - v_next 不存在时 no-op：当前 workdir 已等于 A 时刻状态。
//
// 不在 cp.FileVersions 中的其他文件保持不变（per-edit 的关键性质）。
func (s *PerEditSnapshotStore) Restore(ctx context.Context, checkpointID string) error {
	cp, err := s.readCheckpointMeta(checkpointID)
	if err != nil {
		return err
	}
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	for hash, vA := range cp.FileVersions {
		if err := ctx.Err(); err != nil {
			return err
		}
		nextVersion := s.findNextVersionLocked(hash, vA)
		if nextVersion == 0 {
			continue
		}
		nextMeta, err := s.readVersionMeta(hash, nextVersion)
		if err != nil {
			return fmt.Errorf("per-edit: read meta v%d: %w", nextVersion, err)
		}
		target := s.resolveDisplayPathLocked(hash, nextMeta.DisplayPath)
		if target == "" {
			return fmt.Errorf("per-edit: missing display path for hash %s", hash)
		}
		if !nextMeta.Existed {
			if err := os.RemoveAll(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("per-edit: restore remove %s: %w", target, err)
			}
			continue
		}
		if nextMeta.IsDir {
			if err := os.MkdirAll(target, nextMeta.Mode); err != nil {
				return fmt.Errorf("per-edit: restore mkdir %s: %w", target, err)
			}
			continue
		}
		content, err := s.readVersionBin(hash, nextVersion)
		if err != nil {
			return fmt.Errorf("per-edit: read bin v%d: %w", nextVersion, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("per-edit: restore mkdir parent %s: %w", target, err)
		}
		if err := writeFileAtomic(target, content, nextMeta.Mode); err != nil {
			return fmt.Errorf("per-edit: write restore %s: %w", target, err)
		}
	}
	return nil
}

// RestoreExact 直接恢复 checkpoint 中记录的**精确版本**（不查找 v_next）。
// 用于 UndoRestore：guard checkpoint 保存的就是 restore 前的 pre-write 状态，
// 直接写回即可，无需 v_next 语义。
func (s *PerEditSnapshotStore) RestoreExact(ctx context.Context, checkpointID string) error {
	cp, err := s.readCheckpointMeta(checkpointID)
	if err != nil {
		return err
	}
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	for hash, vAt := range cp.FileVersions {
		if err := ctx.Err(); err != nil {
			return err
		}
		meta, err := s.readVersionMeta(hash, vAt)
		if err != nil {
			return fmt.Errorf("per-edit: read meta v%d: %w", vAt, err)
		}
		target := s.resolveDisplayPathLocked(hash, meta.DisplayPath)
		if target == "" {
			return fmt.Errorf("per-edit: missing display path for hash %s", hash)
		}
		if !meta.Existed {
			if err := os.RemoveAll(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("per-edit: restore-exact remove %s: %w", target, err)
			}
			continue
		}
		if meta.IsDir {
			if err := os.MkdirAll(target, meta.Mode); err != nil {
				return fmt.Errorf("per-edit: restore-exact mkdir %s: %w", target, err)
			}
			continue
		}
		content, err := s.readVersionBin(hash, vAt)
		if err != nil {
			return fmt.Errorf("per-edit: restore-exact read bin v%d: %w", vAt, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("per-edit: restore-exact mkdir parent %s: %w", target, err)
		}
		if err := writeFileAtomic(target, content, meta.Mode); err != nil {
			return fmt.Errorf("per-edit: restore-exact write %s: %w", target, err)
		}
	}
	return nil
}

// Diff 端到端对比两个 checkpoint 之间的工作区差异，返回 unified diff。
// 端到端性质保证：unified diff 算法只看输入端点，中间的反复修改若回到原值会自动从 diff 消失。
func (s *PerEditSnapshotStore) Diff(ctx context.Context, fromID, toID string) (string, error) {
	fromMeta, err := s.readCheckpointMeta(fromID)
	if err != nil {
		return "", err
	}
	toMeta, err := s.readCheckpointMeta(toID)
	if err != nil {
		return "", err
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	hashSet := make(map[string]struct{})
	for h := range fromMeta.FileVersions {
		hashSet[h] = struct{}{}
	}
	for h := range toMeta.FileVersions {
		hashSet[h] = struct{}{}
	}
	hashes := make([]string, 0, len(hashSet))
	for h := range hashSet {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	var buf bytes.Buffer
	for _, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		fromContent, fromIsDir, fromExists, fromDisplay, err := s.contentAtCheckpointLocked(hash, fromMeta.FileVersions, false)
		if err != nil {
			return "", err
		}
		toContent, toIsDir, toExists, toDisplay, err := s.contentAtCheckpointLocked(hash, toMeta.FileVersions, false)
		if err != nil {
			return "", err
		}
		if fromIsDir && toIsDir {
			continue
		}
		if bytes.Equal(fromContent, toContent) && fromExists == toExists && fromIsDir == toIsDir {
			continue
		}
		display := toDisplay
		if display == "" {
			display = fromDisplay
		}
		rel := s.relativeDisplay(display)
		diff := difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(fromContent)),
			B:        difflib.SplitLines(string(toContent)),
			FromFile: "a/" + filepath.ToSlash(rel),
			ToFile:   "b/" + filepath.ToSlash(rel),
			Context:  3,
		}
		out, err := difflib.GetUnifiedDiffString(diff)
		if err != nil {
			return "", fmt.Errorf("per-edit: diff %s: %w", rel, err)
		}
		buf.WriteString(out)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// DeleteCheckpoint 仅删除 cp_<checkpointID>.json 元数据。
// file-history 下的 .bin/.meta 不删除，因为它们可能被其他 checkpoint 引用，GC 由独立流程负责。
func (s *PerEditSnapshotStore) DeleteCheckpoint(checkpointID string) error {
	if checkpointID == "" {
		return nil
	}
	err := os.Remove(s.checkpointMetaPath(checkpointID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// HasPending 返回当前 turn 是否已有 capture 待 Finalize，用于 gate 决定是否创建 checkpoint。
func (s *PerEditSnapshotStore) HasPending() bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	return len(s.pending) > 0
}

// FileChangeKind 表示两个 checkpoint 之间单个 path 的变更类别。
type FileChangeKind string

const (
	FileChangeAdded    FileChangeKind = "added"
	FileChangeDeleted  FileChangeKind = "deleted"
	FileChangeModified FileChangeKind = "modified"
)

// FileChangeEntry 描述端到端 diff 中单个 path 的变更。
type FileChangeEntry struct {
	Path string
	Kind FileChangeKind
}

// ChangedFiles 端到端比较两个 checkpoint，返回 path → 变更类别的列表（按 path 字典序）。
// 不返回内容差异，仅用于 UI 分组（添加/删除/修改）。完整 patch 仍由 Diff 生成。
func (s *PerEditSnapshotStore) ChangedFiles(ctx context.Context, fromID, toID string) ([]FileChangeEntry, error) {
	fromMeta, err := s.readCheckpointMeta(fromID)
	if err != nil {
		return nil, err
	}
	toMeta, err := s.readCheckpointMeta(toID)
	if err != nil {
		return nil, err
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	hashSet := make(map[string]struct{})
	for h := range fromMeta.FileVersions {
		hashSet[h] = struct{}{}
	}
	for h := range toMeta.FileVersions {
		hashSet[h] = struct{}{}
	}
	hashes := make([]string, 0, len(hashSet))
	for h := range hashSet {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	out := make([]FileChangeEntry, 0, len(hashes))
	for _, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fromContent, fromIsDir, fromExists, fromDisplay, err := s.contentAtCheckpointLocked(hash, fromMeta.FileVersions, false)
		if err != nil {
			return nil, err
		}
		toContent, toIsDir, toExists, toDisplay, err := s.contentAtCheckpointLocked(hash, toMeta.FileVersions, false)
		if err != nil {
			return nil, err
		}
		display := toDisplay
		if display == "" {
			display = fromDisplay
		}
		rel := filepath.ToSlash(s.relativeDisplay(display))
		switch {
		case !fromExists && toExists:
			out = append(out, FileChangeEntry{Path: rel, Kind: FileChangeAdded})
		case fromExists && !toExists:
			out = append(out, FileChangeEntry{Path: rel, Kind: FileChangeDeleted})
		case fromIsDir != toIsDir || !bytes.Equal(fromContent, toContent):
			out = append(out, FileChangeEntry{Path: rel, Kind: FileChangeModified})
		}
	}
	return out, nil
}

// PerEditRefPrefix 标识 CheckpointRecord.CodeCheckpointRef 字段中由 per-edit 后端生成的引用。
const PerEditRefPrefix = "peredit:"

// RefForPerEditCheckpoint 返回 per-edit 后端用于 CheckpointRecord.CodeCheckpointRef 的字符串引用。
func RefForPerEditCheckpoint(checkpointID string) string {
	return PerEditRefPrefix + checkpointID
}

// IsPerEditRef 判定一个 CodeCheckpointRef 是否由 per-edit 后端生成。
func IsPerEditRef(ref string) bool {
	return strings.HasPrefix(ref, PerEditRefPrefix)
}

// PerEditCheckpointIDFromRef 从 CodeCheckpointRef 中提取 checkpoint ID。非 per-edit ref 时返回空字符串。
func PerEditCheckpointIDFromRef(ref string) string {
	if !IsPerEditRef(ref) {
		return ""
	}
	return strings.TrimPrefix(ref, PerEditRefPrefix)
}

func perEditPathHash(absPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(absPath)))
	return hex.EncodeToString(sum[:])[:perEditPathHashLen]
}

func readFileForCapture(absPath string) ([]byte, bool, bool, os.FileMode, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, false, 0, nil
		}
		return nil, false, false, 0, err
	}
	if info.IsDir() {
		return nil, true, true, info.Mode(), nil
	}
	if info.Size() > perEditMaxCaptureBytes {
		return nil, true, false, info.Mode(), fmt.Errorf("file %d bytes exceeds per-edit capture limit", info.Size())
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, true, false, info.Mode(), err
	}
	return content, true, false, info.Mode(), nil
}

func (s *PerEditSnapshotStore) writeVersionFiles(meta FileVersionMeta, content []byte) error {
	if err := os.MkdirAll(s.fileHistoryDir, 0o755); err != nil {
		return fmt.Errorf("per-edit: mkdir file-history: %w", err)
	}
	binPath := s.versionBinPath(meta.PathHash, meta.Version)
	metaPath := s.versionMetaPath(meta.PathHash, meta.Version)

	if err := writeFileAtomic(binPath, content, 0o644); err != nil {
		return fmt.Errorf("per-edit: write bin: %w", err)
	}
	if err := s.writeVersionMetaOnly(metaPath, meta); err != nil {
		_ = os.Remove(binPath)
		return err
	}
	return nil
}

func (s *PerEditSnapshotStore) writeVersionMetaOnly(metaPath string, meta FileVersionMeta) error {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("per-edit: marshal meta: %w", err)
	}
	if err := writeFileAtomic(metaPath, metaJSON, 0o644); err != nil {
		return fmt.Errorf("per-edit: write meta: %w", err)
	}
	return nil
}

func writeFileAtomic(target string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, target)
}

func (s *PerEditSnapshotStore) appendIndex(entry perEditIndexEntry) error {
	if err := os.MkdirAll(s.fileHistoryDir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	f, err := os.OpenFile(s.indexPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

func (s *PerEditSnapshotStore) loadIndexFromDisk() {
	f, err := os.Open(s.indexPath())
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry perEditIndexEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		s.pathToVersions[entry.PathHash] = append(s.pathToVersions[entry.PathHash], entry.Version)
		s.displayPaths[entry.PathHash] = entry.DisplayPath
	}
	for hash, versions := range s.pathToVersions {
		sort.Ints(versions)
		s.pathToVersions[hash] = versions
	}
}

func (s *PerEditSnapshotStore) writeCheckpointMeta(meta CheckpointMeta) error {
	if err := os.MkdirAll(s.checkpointsDir, 0o755); err != nil {
		return fmt.Errorf("per-edit: mkdir checkpoints: %w", err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("per-edit: marshal cp meta: %w", err)
	}
	return writeFileAtomic(s.checkpointMetaPath(meta.CheckpointID), data, 0o644)
}

func (s *PerEditSnapshotStore) readCheckpointMeta(checkpointID string) (CheckpointMeta, error) {
	var meta CheckpointMeta
	data, err := os.ReadFile(s.checkpointMetaPath(checkpointID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return meta, fmt.Errorf("per-edit: checkpoint %s not found", checkpointID)
		}
		return meta, fmt.Errorf("per-edit: read cp meta %s: %w", checkpointID, err)
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, fmt.Errorf("per-edit: unmarshal cp meta %s: %w", checkpointID, err)
	}
	if meta.FileVersions == nil {
		meta.FileVersions = map[string]int{}
	}
	return meta, nil
}

func (s *PerEditSnapshotStore) readVersionMeta(hash string, version int) (FileVersionMeta, error) {
	var meta FileVersionMeta
	data, err := os.ReadFile(s.versionMetaPath(hash, version))
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(data, &meta)
	return meta, err
}

func (s *PerEditSnapshotStore) readVersionBin(hash string, version int) ([]byte, error) {
	return os.ReadFile(s.versionBinPath(hash, version))
}

// findNextVersionLocked 返回 hash 下大于 vA 的最小版本号，没有则返回 0。indexMu 必须被持有。
func (s *PerEditSnapshotStore) findNextVersionLocked(hash string, vA int) int {
	versions := s.pathToVersions[hash]
	for _, v := range versions {
		if v > vA {
			return v
		}
	}
	return 0
}

// resolveDisplayPathLocked 选取 hash 对应的工作区绝对路径。indexMu 必须被持有。
func (s *PerEditSnapshotStore) resolveDisplayPathLocked(hash, fallback string) string {
	if dp, ok := s.displayPaths[hash]; ok && dp != "" {
		return dp
	}
	return fallback
}

// contentAtCheckpointLocked 计算 hash 在某个 checkpoint 时刻的 workdir 内容。
// 在 cp.FileVersions 中：找下一版本读 .bin（或 Existed=false 时返回 nil）；
// 没有下一版本时：以当前 workdir 实际内容为准。
// 不在 cp.FileVersions 中且 fallbackIfMissing=false 时：返回 exists=false，避免 diff 侧把工作区当前文件误判为 checkpoint 时刻已存在。
// indexMu 必须被持有。
func (s *PerEditSnapshotStore) contentAtCheckpointLocked(hash string, cpVersions map[string]int, fallbackIfMissing bool) ([]byte, bool, bool, string, error) {
	display := s.displayPaths[hash]
	vAt, ok := cpVersions[hash]
	if !ok {
		if fallbackIfMissing {
			c, isDir, exists := readWorkdirContent(display)
			return c, isDir, exists, display, nil
		}
		return nil, false, false, display, nil
	}
	nextVersion := s.findNextVersionLocked(hash, vAt)
	if nextVersion == 0 {
		c, isDir, exists := readWorkdirContent(display)
		return c, isDir, exists, display, nil
	}
	nextMeta, err := s.readVersionMeta(hash, nextVersion)
	if err != nil {
		return nil, false, false, display, fmt.Errorf("per-edit: read meta v%d for %s: %w", nextVersion, hash, err)
	}
	if !nextMeta.Existed {
		return nil, false, false, display, nil
	}
	if nextMeta.IsDir {
		return nil, true, true, display, nil
	}
	content, err := s.readVersionBin(hash, nextVersion)
	if err != nil {
		return nil, false, false, display, fmt.Errorf("per-edit: read bin v%d for %s: %w", nextVersion, hash, err)
	}
	return content, false, true, display, nil
}

func readWorkdirContent(absPath string) ([]byte, bool, bool) {
	if absPath == "" {
		return nil, false, false
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, false, false
	}
	if info.IsDir() {
		return nil, true, true
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, false, false
	}
	return data, false, true
}

func (s *PerEditSnapshotStore) relativeDisplay(absPath string) string {
	if absPath == "" {
		return ""
	}
	if s.workdir == "" {
		return absPath
	}
	rel, err := filepath.Rel(s.workdir, absPath)
	if err != nil {
		return absPath
	}
	return rel
}

func (s *PerEditSnapshotStore) versionBinPath(hash string, version int) string {
	return filepath.Join(s.fileHistoryDir, fmt.Sprintf("%s@v%d.bin", hash, version))
}

func (s *PerEditSnapshotStore) versionMetaPath(hash string, version int) string {
	return filepath.Join(s.fileHistoryDir, fmt.Sprintf("%s@v%d.meta", hash, version))
}

func (s *PerEditSnapshotStore) checkpointMetaPath(checkpointID string) string {
	return filepath.Join(s.checkpointsDir, fmt.Sprintf("cp_%s.json", checkpointID))
}

func (s *PerEditSnapshotStore) indexPath() string {
	return filepath.Join(s.fileHistoryDir, perEditIndexFileName)
}
