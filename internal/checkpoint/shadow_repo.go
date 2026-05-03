package checkpoint

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	gitCommandTimeout = 5 * time.Second
)

// ShadowRepo 封装 bare git 仓库，用于对用户工作区做代码快照与恢复。
type ShadowRepo struct {
	shadowDir    string
	workdir      string
	gitAvailable bool
	mu           sync.Mutex
}

// NewShadowRepo 创建影子仓库实例，workdir 为用户工作区根目录。
func NewShadowRepo(projectDir string, workdir string) *ShadowRepo {
	return &ShadowRepo{
		shadowDir: filepath.Join(projectDir, ".shadow"),
		workdir:   workdir,
	}
}

// ConflictResult 描述目标 commit 与当前工作区之间的差异。
type ConflictResult struct {
	HasConflict   bool
	AddedFiles    []string
	DeletedFiles  []string
	ModifiedFiles []string
}

// CheckGitAvailability 检查系统是否可用 git 命令。
func CheckGitAvailability(ctx context.Context) (available bool, version string) {
	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "version").CombinedOutput()
	if err != nil {
		return false, ""
	}
	return true, strings.TrimSpace(string(out))
}

// Init 初始化 bare 仓库，设置 core.worktree 指向用户工作区。
// 如果仓库已存在但损坏，会先 Rebuild 再初始化。
func (r *ShadowRepo) Init(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	// 如果目录已存在，先做健康检查
	if _, err := os.Stat(r.shadowDir); err == nil {
		healthCtx, healthCancel := context.WithTimeout(context.Background(), gitCommandTimeout)
		defer healthCancel()
		checkCmd := r.buildGitCommand(healthCtx, "rev-parse", "--git-dir")
		if err := checkCmd.Run(); err != nil {
			// 损坏的仓库，尝试重建
			if rebuildErr := r.rebuildLocked(context.Background()); rebuildErr != nil {
				return fmt.Errorf("checkpoint: rebuild damaged repo: %w", rebuildErr)
			}
		}
	}

	if err := exec.CommandContext(ctx, "git", "init", "--bare", r.shadowDir).Run(); err != nil {
		return fmt.Errorf("checkpoint: init bare repo at %s: %w", r.shadowDir, err)
	}

	// 设置 core.worktree 使后续操作无需重复指定 --work-tree
	ctx2, cancel2 := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel2()
	if err := r.gitExec(ctx2, "config", "core.worktree", r.workdir); err != nil {
		return fmt.Errorf("checkpoint: set core.worktree: %w", err)
	}

	// 设置提交者身份，避免机器上无全局 git 配置时 commit 失败
	ctx3, cancel3 := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel3()
	if err := r.gitExec(ctx3, "config", "user.name", "neocode-checkpoint"); err != nil {
		return fmt.Errorf("checkpoint: set user.name: %w", err)
	}
	ctx4, cancel4 := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel4()
	if err := r.gitExec(ctx4, "config", "user.email", "checkpoint@neocode.local"); err != nil {
		return fmt.Errorf("checkpoint: set user.email: %w", err)
	}

	r.gitAvailable = true
	return nil
}

// IsAvailable 返回影子仓库是否已初始化且 git 可用。
func (r *ShadowRepo) IsAvailable() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gitAvailable
}

// Snapshot 对工作区做快照，返回 commit hash。
// ref 为完整 ref 路径（如 refs/neocode/sessions/<sid>/checkpoints/<id>）。
func (r *ShadowRepo) Snapshot(ctx context.Context, ref string, message string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.gitAvailable {
		return "", fmt.Errorf("checkpoint: shadow repo not available")
	}

	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	if err := r.gitExec(ctx, "add", "-A"); err != nil {
		return "", fmt.Errorf("checkpoint: git add: %w", err)
	}

	commitMsg := message
	if commitMsg == "" {
		commitMsg = "checkpoint snapshot"
	}
	if err := r.gitExec(ctx, "commit", "--allow-empty", "-m", commitMsg); err != nil {
		return "", fmt.Errorf("checkpoint: git commit: %w", err)
	}

	hash, err := r.gitOutput(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("checkpoint: git rev-parse HEAD: %w", err)
	}
	hash = strings.TrimSpace(hash)

	if ref != "" {
		if err := r.gitExec(ctx, "update-ref", ref, hash); err != nil {
			return "", fmt.Errorf("checkpoint: git update-ref %s: %w", ref, err)
		}
	}

	return hash, nil
}

// Restore 将工作区恢复到指定 commit 状态。
func (r *ShadowRepo) Restore(ctx context.Context, commitHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.gitAvailable {
		return fmt.Errorf("checkpoint: shadow repo not available")
	}

	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	if err := r.gitExec(ctx, "checkout", commitHash, "--", "."); err != nil {
		return fmt.Errorf("checkpoint: git checkout %s: %w", commitHash, err)
	}
	return nil
}

// ResolveRef 解析 ref 对应的 commit hash。
func (r *ShadowRepo) ResolveRef(ctx context.Context, ref string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.gitAvailable {
		return "", fmt.Errorf("checkpoint: shadow repo not available")
	}

	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	hash, err := r.gitOutput(ctx, "rev-parse", ref)
	if err != nil {
		return "", fmt.Errorf("checkpoint: resolve ref %s: %w", ref, err)
	}
	return strings.TrimSpace(hash), nil
}

// DeleteRef 删除指定的 ref 引用，用于补偿失败的 checkpoint 创建。
func (r *ShadowRepo) DeleteRef(ctx context.Context, ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.gitAvailable {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	if err := r.gitExec(ctx, "update-ref", "-d", ref); err != nil {
		return fmt.Errorf("checkpoint: git update-ref -d %s: %w", ref, err)
	}
	return nil
}

// HasCodeChanges 检查工作区是否有未提交的代码变更，包括未跟踪文件。
// 先用 git diff --quiet HEAD -- . 检测已跟踪文件变更，再用 ls-files 检测未跟踪文件。
// git 不可用时返回 true（保守策略）。
func (r *ShadowRepo) HasCodeChanges(ctx context.Context) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.gitAvailable {
		return true
	}

	// 检测已跟踪文件变更
	diffCtx, diffCancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer diffCancel()
	cmd := r.buildGitCommand(diffCtx, "diff", "--quiet", "HEAD", "--", ".")
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return true
			}
		}
		return true
	}

	// 检测未跟踪文件（排除 .gitignore 忽略的文件）
	lsCtx, lsCancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer lsCancel()
	out, err := r.gitOutput(lsCtx, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) != ""
}

// HealthCheck 验证 bare 仓库存在且可执行 git 操作。
func (r *ShadowRepo) HealthCheck(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.gitAvailable {
		return fmt.Errorf("checkpoint: shadow repo not available")
	}

	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	if err := r.gitExec(ctx, "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("checkpoint: health check failed: %w", err)
	}
	return nil
}

func (r *ShadowRepo) gitExec(ctx context.Context, args ...string) error {
	cmd := r.buildGitCommand(ctx, args...)
	return cmd.Run()
}

func (r *ShadowRepo) gitOutput(ctx context.Context, args ...string) (string, error) {
	cmd := r.buildGitCommand(ctx, args...)
	out, err := cmd.Output()
	return string(out), err
}

func (r *ShadowRepo) buildGitCommand(ctx context.Context, args ...string) *exec.Cmd {
	fullArgs := make([]string, 0, 4+len(args))
	fullArgs = append(fullArgs, "--git-dir="+r.shadowDir)
	fullArgs = append(fullArgs, "--work-tree="+r.workdir)
	fullArgs = append(fullArgs, args...)
	return exec.CommandContext(ctx, "git", fullArgs...)
}

// DetectConflicts 比较目标 commit 与当前工作区差异。
func (r *ShadowRepo) DetectConflicts(ctx context.Context, commitHash string) (ConflictResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.gitAvailable {
		return ConflictResult{}, fmt.Errorf("checkpoint: shadow repo not available")
	}

	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	out, err := r.gitOutput(ctx, "diff", "--name-status", commitHash, "--", ".")
	if err != nil {
		return ConflictResult{}, fmt.Errorf("checkpoint: git diff: %w", err)
	}

	var result ConflictResult
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		status := parts[0]
		file := parts[1]
		switch {
		case status == "A":
			result.AddedFiles = append(result.AddedFiles, file)
			result.HasConflict = true
		case status == "D":
			result.DeletedFiles = append(result.DeletedFiles, file)
			result.HasConflict = true
		case strings.HasPrefix(status, "M"):
			result.ModifiedFiles = append(result.ModifiedFiles, file)
			result.HasConflict = true
		}
	}
	return result, nil
}

// Rebuild 重建损坏的影子仓库。
func (r *ShadowRepo) Rebuild(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rebuildLocked(ctx)
}

// rebuildLocked 在持有锁的情况下重建影子仓库。
func (r *ShadowRepo) rebuildLocked(ctx context.Context) error {
	backupDir := r.shadowDir + ".bak." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.Rename(r.shadowDir, backupDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("checkpoint: rename old shadow dir: %w", err)
	}
	return nil
}

// RefForCheckpoint 构造 checkpoint 的 git ref 路径。
func RefForCheckpoint(sessionID string, checkpointID string) string {
	return fmt.Sprintf("refs/neocode/sessions/%s/checkpoints/%s", sessionID, checkpointID)
}
