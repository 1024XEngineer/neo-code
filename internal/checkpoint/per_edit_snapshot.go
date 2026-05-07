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
	if checkpointID == "" {
		return false, fmt.Errorf("per-edit: empty checkpointID")
	}

	// 收集版本号（持锁）后释放，再逐文件读 meta 构建快照。
	s.indexMu.Lock()
	if len(s.pathToVersions) == 0 {
		s.indexMu.Unlock()
		return false, nil
	}
	type hashEntry struct {
		hash     string
		versions []int
	}
	entries := make([]hashEntry, 0, len(s.pathToVersions))
	for h, versions := range s.pathToVersions {
		if len(versions) > 0 {
			entries = append(entries, hashEntry{hash: h, versions: versions})
		}
	}
	s.indexMu.Unlock()

	snapshot := make(map[string]int, len(entries))
	for _, e := range entries {
		// 从最新版本往回找，跳过 IsPostDelete=true 的标记版本
		// （post-delete 只记录"文件已删除"，不应用于全量快照）。
		// pre-create 版本（Existed=false, IsPostDelete=false）仍要保留，
		// 否则新建文件在 checkpoint 中将完全不可见。
		for i := len(e.versions) - 1; i >= 0; i-- {
			meta, err := s.readVersionMeta(e.hash, e.versions[i])
			if err != nil || meta.IsPostDelete {
				continue
			}
			snapshot[e.hash] = e.versions[i]
			break
		}
	}

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

// FinalizePending 仅将当前 pending 写入 checkpoint（pre-restore guard 专用）。
// 全量快照会包含多轮前的旧 pre-write 内容，用于 guard 反而会写错状态；
// guard 只需固化为本轮 capture 的增量。
func (s *PerEditSnapshotStore) FinalizePending(checkpointID string) (bool, error) {
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

// Restore 还原工作区至 targetID 对应的 checkpoint 时刻状态。
// guardID 为 pre-restore 固化的快照（restoreCheckpointCore 中的 guard checkpoint），
// 用于对比确定每个文件的目标操作；guardID 为空时仅处理 target checkpoint 内的文件。
//
// 对比逻辑：对 target 与 guard 中出现的每个文件，分别计算"目标状态"与"当前状态"，
// 据此执行写回 / 删除 / 跳过，覆盖文件创建、修改、删除三种变更方向。
func (s *PerEditSnapshotStore) Restore(ctx context.Context, targetID, guardID string) error {
	targetCP, err := s.readCheckpointMeta(targetID)
	if err != nil {
		return err
	}

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	hashSet := make(map[string]struct{}, len(targetCP.FileVersions))
	for h := range targetCP.FileVersions {
		hashSet[h] = struct{}{}
	}

	var guardCP CheckpointMeta
	hasGuard := guardID != ""
	if hasGuard {
		guardCP, err = s.readCheckpointMeta(guardID)
		if err != nil {
			return err
		}
		for h := range guardCP.FileVersions {
			hashSet[h] = struct{}{}
		}
	}
	// 无论有无 guard，都必须合并全量 pathToVersions。
	// guard 是 pending-only 的，不包含此前创建的、本 turn 未触碰的新文件；
	// 不合并则这些文件在 restore 后仍会残留。
	for h := range s.pathToVersions {
		hashSet[h] = struct{}{}
	}

	for hash := range hashSet {
		if err := ctx.Err(); err != nil {
			return err
		}

		// 目标状态：target checkpoint 时刻文件应如何。
		toContent, toIsDir, toExists, toMode, toDisplay, err := s.contentAtCheckpointLocked(hash, targetCP.FileVersions, false)
		if err != nil {
			return err
		}

		// 当前状态：guard checkpoint 时刻（或磁盘现状）。
		var fromContent []byte
		var fromIsDir, fromExists bool
		var fromMode os.FileMode
		var fromDisplay string
		if hasGuard {
			fromContent, fromIsDir, fromExists, fromMode, fromDisplay, err = s.contentAtCheckpointLocked(hash, guardCP.FileVersions, true)
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
func (s *PerEditSnapshotStore) RunAggregateDiff(ctx context.Context, checkpointIDs []string) (string, []FileChangeEntry, error) {
	type versionRange struct {
		minV int
		maxV int
	}
	versionByHash := make(map[string]versionRange)
	var runEndTime time.Time
	for _, cid := range checkpointIDs {
		meta, err := s.readCheckpointMeta(cid)
		if err != nil {
			return "", nil, fmt.Errorf("per-edit: read checkpoint %s: %w", cid, err)
		}
		if meta.CreatedAt.After(runEndTime) {
			runEndTime = meta.CreatedAt
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
		afterContent, afterIsDir, afterExists, degraded := s.contentAfterLastVersionLocked(hash, vr.maxV, display)
		if degraded {
			if info, err := os.Stat(display); err == nil && info.ModTime().After(runEndTime) {
				// run 结束后文件被外部修改，跳过以避免污染
				continue
			}
		}
		// 只有 before 和 after 都是目录时才跳过（unified diff 不支持目录）。
		// 目录删除、目录变文件等变更仍要进入分类，这样 changes 列表能正确反映。
		if beforeIsDir && beforeExists && afterIsDir && afterExists {
			continue
		}
		if afterIsDir {
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
