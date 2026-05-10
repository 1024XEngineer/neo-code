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

	"neo-code/internal/repository"
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
	PathHash     string      `json:"path_hash"`
	DisplayPath  string      `json:"display_path"`
	Version      int         `json:"version"`
	Existed      bool        `json:"existed"`
	IsDir        bool        `json:"is_dir,omitempty"`
	Mode         os.FileMode `json:"mode,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	IsPostDelete bool        `json:"is_post_delete,omitempty"`
}

// CheckpointMeta 是 cp_<checkpointID>.json 的内容。
type CheckpointMeta struct {
	CheckpointID      string         `json:"checkpoint_id"`
	CreatedAt         time.Time      `json:"created_at"`
	FileVersions      map[string]int `json:"file_versions"`
	ExactFileVersions map[string]int `json:"exact_file_versions,omitempty"`
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
			PathHash:     hash,
			DisplayPath:  cleanPath,
			Version:      nextVersion,
			Existed:      false,
			IsDir:        false,
			Mode:         0,
			CreatedAt:    time.Now().UTC(),
			IsPostDelete: true,
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

// Finalize 将当前所有已知文件的(最新版本号→pathHash)映射写入 cp_<checkpointID>.json。
// 每个 checkpoint 均为完整快照（非增量子集），保证任意 checkpoint 回到此点时可完整还原全工作区。
// 跳过 post-delete 版本（Existed=false），因为全量快照应记录文件内容的最近版本号，
// 而非"文件已删除"的占位标记。post-delete 由 v_next 语义在 restore/diff 时查找。
// pathToVersions 为空时返回 (false, nil) 表示目前无文件被追踪过，无需写入。
// 调用方在 Finalize 后应调用 Reset 清空 pending。
func (s *PerEditSnapshotStore) Finalize(checkpointID string) (bool, error) {
	return s.finalizeCheckpoint(checkpointID, false)
}

// FinalizeWithExactState 在写入 pre-write 基线的同时，固化 checkpoint 结束时的精确文件版本。
func (s *PerEditSnapshotStore) FinalizeWithExactState(checkpointID string) (bool, error) {
	return s.finalizeCheckpoint(checkpointID, true)
}

// finalizeCheckpoint 根据需要把 pending 基线与 checkpoint 末状态一并落盘。
func (s *PerEditSnapshotStore) finalizeCheckpoint(checkpointID string, captureExactState bool) (bool, error) {
	if checkpointID == "" {
		return false, fmt.Errorf("per-edit: empty checkpointID")
	}

	s.pendingMu.Lock()
	if len(s.pending) == 0 {
		s.pendingMu.Unlock()
		return false, nil
	}
	snapshot := make(map[string]int, len(s.pending))
	for hash, version := range s.pending {
		if hash == "" || version <= 0 {
			continue
		}
		snapshot[hash] = version
	}
	s.pendingMu.Unlock()
	if len(snapshot) == 0 {
		return false, nil
	}

	var exactSnapshot map[string]int
	if captureExactState {
		var err error
		exactSnapshot, err = s.captureExactStateSnapshot(snapshot)
		if err != nil {
			return false, err
		}
	}

	meta := CheckpointMeta{
		CheckpointID:      checkpointID,
		CreatedAt:         time.Now().UTC(),
		FileVersions:      snapshot,
		ExactFileVersions: exactSnapshot,
	}
	if err := s.writeCheckpointMeta(meta); err != nil {
		return false, err
	}
	return true, nil
}

// FinalizeExactForCheckpoints 为指定检查点影响到的文件捕获当前精确状态，用于 restore guard。
func (s *PerEditSnapshotStore) FinalizeExactForCheckpoints(checkpointID string, checkpointIDs []string) (bool, error) {
	if strings.TrimSpace(checkpointID) == "" {
		return false, fmt.Errorf("per-edit: empty checkpointID")
	}
	baseVersions := make(map[string]int)
	for _, relatedID := range checkpointIDs {
		relatedID = strings.TrimSpace(relatedID)
		if relatedID == "" {
			continue
		}
		cp, err := s.readCheckpointMeta(relatedID)
		if err != nil {
			return false, err
		}
		for hash, version := range cp.FileVersions {
			if strings.TrimSpace(hash) == "" || version <= 0 {
				continue
			}
			baseVersions[hash] = version
		}
		for hash, version := range cp.ExactFileVersions {
			if strings.TrimSpace(hash) == "" || version <= 0 {
				continue
			}
			if _, ok := baseVersions[hash]; !ok {
				baseVersions[hash] = version
			}
		}
	}
	if len(baseVersions) == 0 {
		return false, nil
	}
	exactSnapshot, err := s.captureExactStateSnapshot(baseVersions)
	if err != nil {
		return false, err
	}
	if len(exactSnapshot) == 0 {
		return false, nil
	}
	meta := CheckpointMeta{
		CheckpointID:      checkpointID,
		CreatedAt:         time.Now().UTC(),
		FileVersions:      exactSnapshot,
		ExactFileVersions: exactSnapshot,
	}
	if err := s.writeCheckpointMeta(meta); err != nil {
		return false, err
	}
	return true, nil
}

// captureExactStateSnapshot 为当前 pending 里的每个文件追加一个“checkpoint 结束态”精确版本。
func (s *PerEditSnapshotStore) captureExactStateSnapshot(baseVersions map[string]int) (map[string]int, error) {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	hashes := make([]string, 0, len(baseVersions))
	for hash := range baseVersions {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)

	exactVersions := make(map[string]int, len(hashes))
	for _, hash := range hashes {
		display := s.resolveDisplayPathLocked(hash, "")
		if display == "" {
			meta, err := s.readVersionMeta(hash, baseVersions[hash])
			if err != nil {
				return nil, fmt.Errorf("per-edit: read baseline meta for %s: %w", hash, err)
			}
			display = meta.DisplayPath
		}
		version, err := s.captureExactCurrentVersionLocked(hash, display)
		if err != nil {
			return nil, err
		}
		exactVersions[hash] = version
	}
	return exactVersions, nil
}

// Reset 清空 pending 映射，每轮 turn 开始时调用，避免跨轮残留。
func (s *PerEditSnapshotStore) Reset() {
	s.pendingMu.Lock()
	s.pending = make(map[string]int)
	s.pendingMu.Unlock()
}

// Restore 还原工作区至 targetID 对应的 checkpoint 时刻状态。
// guardID 为 pre-restore 固化的快照（restoreCheckpointCore 中的 guard checkpoint），
// 用于对比确定每个文件的目标操作；guardID 为空时仅处理 target checkpoint 内的文件。
//
// 对比逻辑：存在 relatedCheckpointIDs 时先收敛到 target 与后续 checkpoint 的净变化路径，
// 再分别计算"目标状态"与"当前状态"并执行写回 / 删除 / 跳过。
func (s *PerEditSnapshotStore) Restore(ctx context.Context, targetID, guardID string, relatedCheckpointIDs ...string) error {
	targetCP, err := s.readCheckpointMeta(targetID)
	if err != nil {
		return err
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	var guardCP CheckpointMeta
	hasGuard := guardID != ""
	if hasGuard {
		guardCP, err = s.readCheckpointMeta(guardID)
		if err != nil {
			return err
		}
	}
	relatedCPs := make([]CheckpointMeta, 0, len(relatedCheckpointIDs))
	for _, checkpointID := range relatedCheckpointIDs {
		if strings.TrimSpace(checkpointID) == "" || checkpointID == targetID || checkpointID == guardID {
			continue
		}
		relatedCP, err := s.readCheckpointMeta(checkpointID)
		if err != nil {
			return err
		}
		relatedCPs = append(relatedCPs, relatedCP)
	}

	hashSet := make(map[string]struct{})
	addCheckpointHashes(hashSet, targetCP)
	for _, relatedCP := range relatedCPs {
		addCheckpointHashes(hashSet, relatedCP)
	}
	if hasGuard {
		addCheckpointHashes(hashSet, guardCP)
	}

	for hash := range hashSet {
		if err := ctx.Err(); err != nil {
			return err
		}

		// 目标状态：target checkpoint 时刻文件应如何。
		toContent, toIsDir, toExists, toMode, toDisplay, err := s.contentAtCheckpointStateLocked(hash, targetCP, false)
		if err != nil {
			return err
		}

		// 当前状态：guard checkpoint 时刻（或磁盘现状）。
		var fromContent []byte
		var fromIsDir, fromExists bool
		var fromMode os.FileMode
		var fromDisplay string
		if hasGuard {
			fromContent, fromIsDir, fromExists, fromMode, fromDisplay, err = s.contentAtCheckpointStateLocked(hash, guardCP, true)
			if err != nil {
				return err
			}
		} else {
			display := s.resolveDisplayPathLocked(hash, "")
			fromContent, fromIsDir, fromExists = readWorkdirContent(display)
			fromMode = readWorkdirMode(display)
			fromDisplay = display
		}

		display := toDisplay
		if display == "" {
			display = fromDisplay
		}
		if display == "" {
			continue
		}

		if toExists == fromExists && toIsDir == fromIsDir && bytes.Equal(toContent, fromContent) && toMode == fromMode {
			continue
		}

		// 类型不一致时，先移除旧节点（文件变目录 或 目录变文件）
		if toExists && toIsDir != fromIsDir && fromExists {
			if err := os.RemoveAll(display); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("per-edit: restore remove type-mismatch %s: %w", display, err)
			}
			fromExists = false
		}

		if !toExists {
			if err := os.RemoveAll(display); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("per-edit: restore remove %s: %w", display, err)
			}
			continue
		}

		if toIsDir {
			if toMode == 0 {
				toMode = 0o755
			}
			if err := os.MkdirAll(display, toMode); err != nil {
				return fmt.Errorf("per-edit: restore mkdir %s: %w", display, err)
			}
			// 目录已存在但权限不同，需要修正
			if fromExists && fromMode != toMode {
				if err := os.Chmod(display, toMode); err != nil {
					return fmt.Errorf("per-edit: restore chmod %s: %w", display, err)
				}
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(display), 0o755); err != nil {
			return fmt.Errorf("per-edit: restore mkdir parent %s: %w", display, err)
		}
		if err := writeFileAtomic(display, toContent, toMode); err != nil {
			return fmt.Errorf("per-edit: restore write %s: %w", display, err)
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

	hashSet := make(map[string]struct{}, len(cp.FileVersions)+len(cp.ExactFileVersions))
	for hash := range cp.FileVersions {
		hashSet[hash] = struct{}{}
	}
	for hash := range cp.ExactFileVersions {
		hashSet[hash] = struct{}{}
	}
	hashes := make([]string, 0, len(hashSet))
	for hash := range hashSet {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	for _, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return err
		}
		vAt, ok := cp.ExactFileVersions[hash]
		if !ok {
			vAt = cp.FileVersions[hash]
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

// RestoreBaseline 按 checkpoint 的 FileVersions 还原指定相对路径到首次触碰前基线。
// 该函数仅读取 FileVersions，不读取 ExactFileVersions，也不使用 v_next 推断语义。
func (s *PerEditSnapshotStore) RestoreBaseline(ctx context.Context, checkpointID string, relPaths []string) error {
	if s == nil {
		return fmt.Errorf("per-edit: store not available")
	}
	if strings.TrimSpace(checkpointID) == "" {
		return fmt.Errorf("per-edit: checkpoint id required")
	}
	if len(relPaths) == 0 {
		return fmt.Errorf("per-edit: baseline restore paths required")
	}
	cp, err := s.readCheckpointMeta(checkpointID)
	if err != nil {
		return err
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	for _, relPath := range relPaths {
		if err := ctx.Err(); err != nil {
			return err
		}
		cleanRel := filepath.ToSlash(strings.TrimSpace(relPath))
		for strings.HasPrefix(cleanRel, "./") {
			cleanRel = strings.TrimPrefix(cleanRel, "./")
		}
		if cleanRel == "" || cleanRel == "." {
			return fmt.Errorf("per-edit: invalid restore path %q", relPath)
		}
		absPath := filepath.Clean(filepath.Join(s.workdir, filepath.FromSlash(cleanRel)))
		relToWorkdir, relErr := filepath.Rel(filepath.Clean(s.workdir), absPath)
		if relErr != nil || relToWorkdir == ".." || strings.HasPrefix(relToWorkdir, ".."+string(filepath.Separator)) {
			return fmt.Errorf("per-edit: restore path %q escapes workdir", relPath)
		}

		hash := perEditPathHash(absPath)
		version, ok := cp.FileVersions[hash]
		if !ok || version <= 0 {
			return fmt.Errorf("per-edit: baseline version for path %s not found in checkpoint %s", cleanRel, checkpointID)
		}
		meta, err := s.readVersionMeta(hash, version)
		if err != nil {
			return fmt.Errorf("per-edit: read baseline meta v%d for %s: %w", version, cleanRel, err)
		}
		if !meta.Existed {
			if err := os.RemoveAll(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("per-edit: baseline remove %s: %w", cleanRel, err)
			}
			continue
		}
		if meta.IsDir {
			mode := meta.Mode
			if mode == 0 {
				mode = 0o755
			}
			if err := os.MkdirAll(absPath, mode); err != nil {
				return fmt.Errorf("per-edit: baseline mkdir %s: %w", cleanRel, err)
			}
			if err := os.Chmod(absPath, mode); err != nil {
				return fmt.Errorf("per-edit: baseline chmod %s: %w", cleanRel, err)
			}
			continue
		}
		content, err := s.readVersionBin(hash, version)
		if err != nil {
			return fmt.Errorf("per-edit: baseline read bin v%d for %s: %w", version, cleanRel, err)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("per-edit: baseline mkdir parent %s: %w", cleanRel, err)
		}
		if err := writeFileAtomic(absPath, content, meta.Mode); err != nil {
			return fmt.Errorf("per-edit: baseline write %s: %w", cleanRel, err)
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
		fromContent, fromIsDir, fromExists, _, fromDisplay, err := s.contentAtCheckpointLocked(hash, fromMeta.FileVersions, false)
		if err != nil {
			return "", err
		}
		toContent, toIsDir, toExists, _, toDisplay, err := s.contentAtCheckpointLocked(hash, toMeta.FileVersions, false)
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

// RunEndCapture 在 run 结束时为本次 run 涉及的所有文件创建快照版本。
// 这些快照版本直接追加到版本链（不进入 pending），确保 RunAggregateDiff 的
// after-side 不再退化到当前 workdir，彻底隔离 run 结束后外部删除/修改的影响。
// checkpointIDs 应为 PerEditCheckpointIDFromRef 提取后的值（不含 "peredit:" 前缀）。
func (s *PerEditSnapshotStore) RunEndCapture(ctx context.Context, checkpointIDs []string) error {
	hashSet := make(map[string]struct{})
	for _, cid := range checkpointIDs {
		meta, err := s.readCheckpointMeta(cid)
		if err != nil {
			continue
		}
		for hash := range meta.FileVersions {
			hashSet[hash] = struct{}{}
		}
	}
	if len(hashSet) == 0 {
		return nil
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	for hash := range hashSet {
		if err := ctx.Err(); err != nil {
			return err
		}

		display := s.displayPaths[hash]
		if display == "" {
			continue
		}

		content, existed, isDir, mode, err := readFileForCapture(display)
		if err != nil {
			continue
		}

		versions := s.pathToVersions[hash]
		nextVersion := 1
		if len(versions) > 0 {
			nextVersion = versions[len(versions)-1] + 1
		}

		vm := FileVersionMeta{
			PathHash:    hash,
			DisplayPath: display,
			Version:     nextVersion,
			Existed:     existed,
			IsDir:       isDir,
			Mode:        mode,
			CreatedAt:   time.Now().UTC(),
		}

		if err := s.writeVersionFiles(vm, content); err != nil {
			continue
		}
		if err := s.appendIndex(perEditIndexEntry{
			PathHash:    hash,
			DisplayPath: display,
			Version:     nextVersion,
		}); err != nil {
			continue
		}

		s.pathToVersions[hash] = append(versions, nextVersion)
	}
	return nil
}

// RunAggregateDiff 计算一次 run-scoped 聚合 diff：
// 对给定 per-edit checkpointIDs 覆盖的每个文件：
//   - before: 取最小版本号的 v.bin 作为首次触碰前的基线
//   - after:  取最大版本号，对其应用 v_next 语义（若 v_next 存在则以 v_next.bin
//     作为 run 结束时的文件状态；否则退化到 workdir）。
//
// 限制：当 run 的最后一次写入是版本链末端且无后续 capture 时，after-side 会退化到
// 当前 workdir。若 run 结束后用户手动修改了该文件，这些修改会混入 diff。
// 此时若文件 mtime 晚于 run 最后一个 checkpoint 的创建时间，该文件会被跳过并记录警告。
//
// checkpointIDs 应为 PerEditCheckpointIDFromRef 提取后的值（不含 "peredit:" 前缀）。
//
// prevFileVersions 保留为兼容参数；run-touched 语义下不再用跨 run 基线过滤文件。
func (s *PerEditSnapshotStore) RunAggregateDiff(ctx context.Context, checkpointIDs []string, prevFileVersions map[string]int) (string, []FileChangeEntry, error) {
	type versionRange struct {
		minV int
		maxV int
	}
	versionByHash := make(map[string]versionRange)
	_ = prevFileVersions
	for _, cid := range checkpointIDs {
		meta, err := s.readCheckpointMeta(cid)
		if err != nil {
			return "", nil, fmt.Errorf("per-edit: read checkpoint %s: %w", cid, err)
		}
		for hash, v := range meta.FileVersions {
			if vr, ok := versionByHash[hash]; ok {
				if v < vr.minV {
					vr.minV = v
				}
				if v > vr.maxV {
					vr.maxV = v
				}
				versionByHash[hash] = vr
			} else {
				versionByHash[hash] = versionRange{minV: v, maxV: v}
			}
		}
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	hashes := make([]string, 0, len(versionByHash))
	for h := range versionByHash {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	var buf bytes.Buffer
	changes := make([]FileChangeEntry, 0, len(hashes))
	for _, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		vr := versionByHash[hash]
		vmeta, err := s.readVersionMeta(hash, vr.minV)
		if err != nil {
			return "", nil, fmt.Errorf("per-edit: read baseline meta v%d %s: %w", vr.minV, hash, err)
		}
		display := s.resolveDisplayPathLocked(hash, vmeta.DisplayPath)
		if display == "" {
			return "", nil, fmt.Errorf("per-edit: missing display path for hash %s", hash)
		}
		var beforeContent []byte
		beforeExists := vmeta.Existed
		beforeIsDir := vmeta.IsDir
		if beforeExists && !beforeIsDir {
			beforeContent, err = s.readVersionBin(hash, vr.minV)
			if err != nil {
				return "", nil, fmt.Errorf("per-edit: read baseline bin v%d %s: %w", vr.minV, hash, err)
			}
		}
		afterContent, afterIsDir, afterExists, _ := s.contentAfterLastVersionLocked(hash, vr.maxV, display)
		// 只有 before 和 after 都是目录时才跳过（unified diff 不支持目录）。
		// 目录删除、目录变文件等变更仍要进入分类，这样 changes 列表能正确反映。
		if beforeIsDir && beforeExists && afterIsDir && afterExists {
			continue
		}
		if beforeIsDir && afterIsDir {
			continue
		}
		if beforeExists == afterExists && bytes.Equal(beforeContent, afterContent) {
			continue
		}
		var kind FileChangeKind
		switch {
		case !beforeExists && afterExists:
			kind = FileChangeAdded
		case beforeExists && !afterExists:
			kind = FileChangeDeleted
		default:
			kind = FileChangeModified
		}
		rel := filepath.ToSlash(s.relativeDisplay(display))
		changes = append(changes, FileChangeEntry{Path: rel, Kind: kind})
		diff := difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(beforeContent)),
			B:        difflib.SplitLines(string(afterContent)),
			FromFile: "a/" + rel,
			ToFile:   "b/" + rel,
			Context:  3,
		}
		out, err := difflib.GetUnifiedDiffString(diff)
		if err != nil {
			return "", nil, fmt.Errorf("per-edit: aggregate diff %s: %w", rel, err)
		}
		buf.WriteString(out)
	}
	return strings.TrimRight(buf.String(), "\n"), changes, nil
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

// FileChangeKind 是 repository.FileChangeKind 的别名，保留以维持向后兼容。
type FileChangeKind = repository.FileChangeKind

const (
	FileChangeAdded    = repository.FileChangeAdded
	FileChangeDeleted  = repository.FileChangeDeleted
	FileChangeModified = repository.FileChangeModified
)

// FileChangeEntry 是 repository.FileChangeEntry 的别名，保留以维持向后兼容。
type FileChangeEntry = repository.FileChangeEntry

// DiffCheckpointsToWorkdir 按多个 checkpoint 的首次触碰版本作为基线，对比当前工作区最终状态。
func (s *PerEditSnapshotStore) DiffCheckpointsToWorkdir(ctx context.Context, checkpointIDs []string) (string, []FileChangeEntry, error) {
	if s == nil {
		return "", nil, fmt.Errorf("per-edit: store not available")
	}
	baseVersions := make(map[string]int)
	for _, checkpointID := range checkpointIDs {
		cp, err := s.readCheckpointMeta(checkpointID)
		if err != nil {
			return "", nil, err
		}
		for hash, version := range cp.FileVersions {
			if _, exists := baseVersions[hash]; !exists {
				baseVersions[hash] = version
			}
		}
	}
	if len(baseVersions) == 0 {
		return "", nil, nil
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	hashes := make([]string, 0, len(baseVersions))
	for hash := range baseVersions {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)

	var patch bytes.Buffer
	changes := make([]FileChangeEntry, 0, len(hashes))
	for _, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		fromContent, fromIsDir, fromExists, fromDisplay, err := s.contentAtExactVersionLocked(hash, baseVersions[hash])
		if err != nil {
			return "", nil, err
		}
		display := s.resolveDisplayPathLocked(hash, fromDisplay)
		toContent, toIsDir, toExists := readWorkdirContent(display)
		if fromIsDir && toIsDir {
			continue
		}
		if bytes.Equal(fromContent, toContent) && fromExists == toExists && fromIsDir == toIsDir {
			continue
		}
		rel := filepath.ToSlash(s.relativeDisplay(display))
		kind := classifyFileChange(fromContent, fromIsDir, fromExists, toContent, toIsDir, toExists)
		if kind != "" {
			changes = append(changes, FileChangeEntry{Path: rel, Kind: kind})
		}
		out, err := unifiedDiffForContents(rel, fromContent, toContent)
		if err != nil {
			return "", nil, err
		}
		patch.WriteString(out)
	}
	return strings.TrimRight(patch.String(), "\n"), changes, nil
}

// DiffCheckpointsToCheckpoint 汇总多个 checkpoint 的首触碰基线，并对比目标 checkpoint 的精确结束态。
func (s *PerEditSnapshotStore) DiffCheckpointsToCheckpoint(
	ctx context.Context,
	checkpointIDs []string,
	targetCheckpointID string,
) (string, []FileChangeEntry, error) {
	if s == nil {
		return "", nil, fmt.Errorf("per-edit: store not available")
	}
	if strings.TrimSpace(targetCheckpointID) == "" {
		return "", nil, fmt.Errorf("per-edit: target checkpoint id required")
	}

	baseVersions := make(map[string]int)
	for _, checkpointID := range checkpointIDs {
		cp, err := s.readCheckpointMeta(checkpointID)
		if err != nil {
			return "", nil, err
		}
		for hash, version := range cp.FileVersions {
			if _, exists := baseVersions[hash]; !exists {
				baseVersions[hash] = version
			}
		}
	}
	if len(baseVersions) == 0 {
		return "", nil, nil
	}

	targetMeta, err := s.readCheckpointMeta(targetCheckpointID)
	if err != nil {
		return "", nil, err
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	hashes := make([]string, 0, len(baseVersions))
	for hash := range baseVersions {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)

	var patch bytes.Buffer
	changes := make([]FileChangeEntry, 0, len(hashes))
	for _, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		fromContent, fromIsDir, fromExists, fromDisplay, err := s.contentAtExactVersionLocked(hash, baseVersions[hash])
		if err != nil {
			return "", nil, err
		}
		toContent, toIsDir, toExists, toDisplay, err := s.contentAtCheckpointTargetLocked(hash, targetMeta, fromDisplay)
		if err != nil {
			return "", nil, err
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
		rel := filepath.ToSlash(s.relativeDisplay(display))
		kind := classifyFileChange(fromContent, fromIsDir, fromExists, toContent, toIsDir, toExists)
		if kind != "" {
			changes = append(changes, FileChangeEntry{Path: rel, Kind: kind})
		}
		out, err := unifiedDiffForContents(rel, fromContent, toContent)
		if err != nil {
			return "", nil, err
		}
		patch.WriteString(out)
	}
	return strings.TrimRight(patch.String(), "\n"), changes, nil
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
		fromContent, fromIsDir, fromExists, _, fromDisplay, err := s.contentAtCheckpointLocked(hash, fromMeta.FileVersions, false)
		if err != nil {
			return nil, err
		}
		toContent, toIsDir, toExists, _, toDisplay, err := s.contentAtCheckpointLocked(hash, toMeta.FileVersions, false)
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

// GetCheckpointFileVersions 读取指定 checkpoint 的 FileVersions 映射，供兼容路径和测试使用。
func (s *PerEditSnapshotStore) GetCheckpointFileVersions(checkpointID string) (map[string]int, error) {
	meta, err := s.readCheckpointMeta(checkpointID)
	if err != nil {
		return nil, err
	}
	return meta.FileVersions, nil
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
	if meta.ExactFileVersions == nil {
		meta.ExactFileVersions = map[string]int{}
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

// captureExactCurrentVersionLocked 读取当前工作区状态，并为同一路径追加一个精确版本。
func (s *PerEditSnapshotStore) captureExactCurrentVersionLocked(hash, displayPath string) (int, error) {
	cleanPath := filepath.Clean(displayPath)
	if cleanPath == "" || cleanPath == "." {
		return 0, fmt.Errorf("per-edit: missing display path for exact snapshot")
	}

	versions := s.pathToVersions[hash]
	nextVersion := 1
	if len(versions) > 0 {
		nextVersion = versions[len(versions)-1] + 1
	}

	content, existed, isDir, mode, err := readFileForCapture(cleanPath)
	if err != nil {
		return 0, fmt.Errorf("per-edit: read exact state %s: %w", cleanPath, err)
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
		return 0, fmt.Errorf("per-edit: append exact index: %w", err)
	}

	s.pathToVersions[hash] = append(versions, nextVersion)
	s.displayPaths[hash] = cleanPath
	return nextVersion, nil
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

// readWorkdirMode 读取 workdir 上 absPath 的文件权限，失败时返回 0。
func readWorkdirMode(absPath string) os.FileMode {
	if absPath == "" {
		return 0
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return 0
	}
	return info.Mode()
}

// addCheckpointHashes 把 checkpoint 中显式记录的文件 hash 加入 restore 候选集合。
func addCheckpointHashes(hashSet map[string]struct{}, cp CheckpointMeta) {
	for hash := range cp.FileVersions {
		hashSet[hash] = struct{}{}
	}
	for hash := range cp.ExactFileVersions {
		hashSet[hash] = struct{}{}
	}
}

// contentAtCheckpointStateLocked 读取 checkpoint 结束态；新 checkpoint 优先使用 ExactFileVersions。
// 旧 checkpoint 没有 exact 版本时，继续使用 v_next 兼容语义。
func (s *PerEditSnapshotStore) contentAtCheckpointStateLocked(
	hash string,
	cp CheckpointMeta,
	fallbackIfMissing bool,
) ([]byte, bool, bool, os.FileMode, string, error) {
	if version, ok := cp.ExactFileVersions[hash]; ok {
		meta, err := s.readVersionMeta(hash, version)
		if err != nil {
			return nil, false, false, 0, "", fmt.Errorf("per-edit: read exact meta v%d for %s: %w", version, hash, err)
		}
		display := s.resolveDisplayPathLocked(hash, meta.DisplayPath)
		if !meta.Existed {
			return nil, false, false, meta.Mode, display, nil
		}
		if meta.IsDir {
			return nil, true, true, meta.Mode, display, nil
		}
		content, err := s.readVersionBin(hash, version)
		if err != nil {
			return nil, false, false, 0, display, fmt.Errorf("per-edit: read exact bin v%d for %s: %w", version, hash, err)
		}
		return content, false, true, meta.Mode, display, nil
	}
	return s.contentAtCheckpointLocked(hash, cp.FileVersions, fallbackIfMissing)
}

// contentAtCheckpointLocked 计算 hash 在某个 checkpoint 时刻的 workdir 内容。
// 在 cp.FileVersions 中：找下一版本读 .bin（或 Existed=false 时返回 nil）；
// 没有下一版本时：以当前 workdir 实际内容为准。
// 不在 cp.FileVersions 中且 fallbackIfMissing=false 时：返回 exists=false，避免 diff 侧把工作区当前文件误判为 checkpoint 时刻已存在。
// indexMu 必须被持有。
func (s *PerEditSnapshotStore) contentAtCheckpointLocked(hash string, cpVersions map[string]int, fallbackIfMissing bool) ([]byte, bool, bool, os.FileMode, string, error) {
	display := s.displayPaths[hash]
	vAt, ok := cpVersions[hash]
	if !ok {
		if fallbackIfMissing {
			c, isDir, exists := readWorkdirContent(display)
			mode := readWorkdirMode(display)
			return c, isDir, exists, mode, display, nil
		}
		return nil, false, false, 0, display, nil
	}
	nextVersion := s.findNextVersionLocked(hash, vAt)
	if nextVersion == 0 {
		c, isDir, exists := readWorkdirContent(display)
		mode := readWorkdirMode(display)
		return c, isDir, exists, mode, display, nil
	}
	nextMeta, err := s.readVersionMeta(hash, nextVersion)
	if err != nil {
		return nil, false, false, 0, display, fmt.Errorf("per-edit: read meta v%d for %s: %w", nextVersion, hash, err)
	}
	if !nextMeta.Existed {
		return nil, false, false, 0, display, nil
	}
	if nextMeta.IsDir {
		return nil, true, true, nextMeta.Mode, display, nil
	}
	content, err := s.readVersionBin(hash, nextVersion)
	if err != nil {
		return nil, false, false, 0, display, fmt.Errorf("per-edit: read bin v%d for %s: %w", nextVersion, hash, err)
	}
	return content, false, true, nextMeta.Mode, display, nil
}

// contentAtExactVersionLocked 读取指定 hash/version 保存的精确内容，调用方必须持有 indexMu。
func (s *PerEditSnapshotStore) contentAtExactVersionLocked(hash string, version int) ([]byte, bool, bool, string, error) {
	meta, err := s.readVersionMeta(hash, version)
	if err != nil {
		return nil, false, false, "", fmt.Errorf("per-edit: read exact meta v%d for %s: %w", version, hash, err)
	}
	display := s.resolveDisplayPathLocked(hash, meta.DisplayPath)
	if !meta.Existed {
		return nil, false, false, display, nil
	}
	if meta.IsDir {
		return nil, true, true, display, nil
	}
	content, err := s.readVersionBin(hash, version)
	if err != nil {
		return nil, false, false, display, fmt.Errorf("per-edit: read exact bin v%d for %s: %w", version, hash, err)
	}
	return content, false, true, display, nil
}

// contentAtCheckpointTargetLocked 优先读取 checkpoint 记录的精确结束态，缺失时兼容回退到当前工作区。
func (s *PerEditSnapshotStore) contentAtCheckpointTargetLocked(
	hash string,
	cp CheckpointMeta,
	fallbackDisplay string,
) ([]byte, bool, bool, string, error) {
	if version, ok := cp.ExactFileVersions[hash]; ok {
		return s.contentAtExactVersionLocked(hash, version)
	}
	display := s.resolveDisplayPathLocked(hash, fallbackDisplay)
	content, isDir, exists := readWorkdirContent(display)
	return content, isDir, exists, display, nil
}

// classifyFileChange 将端点状态归类为 UI 可展示的 added/deleted/modified。
func classifyFileChange(
	fromContent []byte,
	fromIsDir bool,
	fromExists bool,
	toContent []byte,
	toIsDir bool,
	toExists bool,
) FileChangeKind {
	switch {
	case !fromExists && toExists:
		return FileChangeAdded
	case fromExists && !toExists:
		return FileChangeDeleted
	case fromIsDir != toIsDir || !bytes.Equal(fromContent, toContent):
		return FileChangeModified
	default:
		return ""
	}
}

// unifiedDiffForContents 生成单个文件的 unified diff 片段，路径已按工作区相对路径传入。
func unifiedDiffForContents(rel string, fromContent, toContent []byte) (string, error) {
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
	return out, nil
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

// contentAfterLastVersionLocked 返回文件在 run 结束时的状态：
// 以 v_last 为版本号，通过 v_next 语义找到 run 后的首次工具触碰前快照；
// 若无后续触碰则退回 readWorkdirContent。indexMu 必须被持有。
// 返回值最后一个是 degraded 标记：true 表示因 nextV==0 或读失败而退化到 workdir。
func (s *PerEditSnapshotStore) contentAfterLastVersionLocked(hash string, vLast int, display string) ([]byte, bool, bool, bool) {
	nextV := s.findNextVersionLocked(hash, vLast)
	if nextV == 0 {
		c, isDir, exists := readWorkdirContent(display)
		return c, isDir, exists, true
	}
	nextMeta, err := s.readVersionMeta(hash, nextV)
	if err != nil {
		c, isDir, exists := readWorkdirContent(display)
		return c, isDir, exists, true
	}
	if !nextMeta.Existed {
		return nil, false, false, false
	}
	if nextMeta.IsDir {
		return nil, true, true, false
	}
	content, err := s.readVersionBin(hash, nextV)
	if err != nil {
		c, isDir, exists := readWorkdirContent(display)
		return c, isDir, exists, true
	}
	return content, false, true, false
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
