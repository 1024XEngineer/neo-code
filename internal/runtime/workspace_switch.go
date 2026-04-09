package runtime

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	agentsession "neo-code/internal/session"
)

// WorkspaceSwitchInput 描述一次工作区切换请求。
type WorkspaceSwitchInput struct {
	SessionID     string
	RequestedPath string
}

// WorkspaceSwitchResult 汇总一次工作区切换后的可见状态。
type WorkspaceSwitchResult struct {
	WorkspaceRoot    string
	Workdir          string
	WorkspaceChanged bool
	KeepSession      bool
	ResetToDraft     bool
	Session          agentsession.Session
}

var workspaceGitRootResolver = detectWorkspaceGitRoot

// workspaceStoreProvider 描述 runtime 所需的按工作区获取会话存储能力。
type workspaceStoreProvider interface {
	StoreForWorkspace(workspaceRoot string) agentsession.Store
}

// fixedWorkspaceStoreProvider 让单一 store 也能适配按工作区路由的读取逻辑。
type fixedWorkspaceStoreProvider struct {
	store agentsession.Store
}

// StoreForWorkspace 返回固定的单一会话存储实例。
func (p fixedWorkspaceStoreProvider) StoreForWorkspace(string) agentsession.Store {
	return p.store
}

// effectiveWorkspaceBase 返回解析目标路径时应使用的当前目录。
func effectiveWorkspaceBase(currentWorkdir string, workspaceRoot string) string {
	base := strings.TrimSpace(currentWorkdir)
	if base != "" {
		return base
	}
	return strings.TrimSpace(workspaceRoot)
}

// resolveWorkspaceSelection 解析目标目录并推导所属工作区根目录。
func resolveWorkspaceSelection(ctx context.Context, baseWorkdir string, requestedPath string) (string, string, error) {
	workdir, err := resolveRequestedWorkspacePath(baseWorkdir, requestedPath)
	if err != nil {
		return "", "", err
	}

	root := workdir
	if resolver := workspaceGitRootResolver; resolver != nil {
		if gitRoot, resolveErr := resolver(ctx, workdir); resolveErr == nil && strings.TrimSpace(gitRoot) != "" {
			root = gitRoot
		}
	}

	root, err = normalizeExistingWorkdir(root)
	if err != nil {
		return "", "", err
	}
	return workdir, root, nil
}

// resolveRunWorkspaceStore 根据本次 Run 输入预先确定应使用的会话分桶与默认工作目录。
func (s *Service) resolveRunWorkspaceStore(ctx context.Context, input UserInput) (agentsession.Store, string, string, error) {
	currentRoot, currentWorkdir := s.currentWorkspaceState()
	workspaceRoot := currentRoot
	defaultWorkdir := effectiveWorkspaceBase(currentWorkdir, currentRoot)

	if strings.TrimSpace(input.SessionID) == "" && strings.TrimSpace(input.Workdir) != "" {
		_, resolvedRoot, err := resolveWorkspaceSelection(ctx, defaultWorkdir, input.Workdir)
		if err != nil {
			return nil, "", "", err
		}
		workspaceRoot = resolvedRoot
	}

	store := s.sessionStoreForWorkspace(workspaceRoot)
	if store == nil {
		return nil, "", "", errors.New("runtime: session store is nil")
	}
	return store, workspaceRoot, defaultWorkdir, nil
}

// resolveRequestedWorkspacePath 将请求路径解析为存在的绝对目录。
func resolveRequestedWorkspacePath(baseWorkdir string, requestedPath string) (string, error) {
	base, err := normalizeExistingWorkdir(baseWorkdir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(requestedPath) == "" {
		return base, nil
	}

	target := strings.TrimSpace(requestedPath)
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	return normalizeExistingWorkdir(target)
}

// detectWorkspaceGitRoot 尝试解析目录所属 Git 仓库根目录。
func detectWorkspaceGitRoot(ctx context.Context, workdir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	current, err := normalizeExistingWorkdir(workdir)
	if err != nil {
		return "", err
	}

	command := exec.CommandContext(ctx, "git", "-C", current, "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		return "", errors.New("runtime: directory is not inside git work tree")
	}

	root, err := normalizeExistingWorkdir(strings.TrimSpace(string(output)))
	if err != nil {
		return "", err
	}
	if isFilesystemRoot(root) && !sameWorkspacePath(root, current) {
		return "", errors.New("runtime: directory is not inside git work tree")
	}
	return root, nil
}

// isFilesystemRoot 判断路径是否已经位于文件系统根目录。
func isFilesystemRoot(path string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" {
		return false
	}
	return filepath.Dir(cleaned) == cleaned
}

// sameWorkspacePath 判断两个工作区路径在当前平台上是否表示同一目录。
func sameWorkspacePath(left string, right string) bool {
	normalizedLeft := normalizeWorkspacePathKey(left)
	normalizedRight := normalizeWorkspacePathKey(right)
	return normalizedLeft != "" && normalizedLeft == normalizedRight
}

// normalizeWorkspacePathKey 生成用于比较工作区路径的稳定键。
func normalizeWorkspacePathKey(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}

	absolute, err := filepath.Abs(trimmed)
	if err == nil {
		trimmed = absolute
	}
	trimmed = filepath.Clean(trimmed)
	if goruntime.GOOS == "windows" {
		return strings.ToLower(trimmed)
	}
	return trimmed
}

// currentWorkspaceState 读取 runtime 当前持有的工作区根目录和工作目录。
func (s *Service) currentWorkspaceState() (string, string) {
	s.workspaceMu.RLock()
	defer s.workspaceMu.RUnlock()
	return strings.TrimSpace(s.activeWorkspaceRoot), strings.TrimSpace(s.activeWorkdir)
}

// setWorkspaceState 更新 runtime 当前持有的工作区根目录和工作目录。
func (s *Service) setWorkspaceState(workspaceRoot string, workdir string) {
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	s.activeWorkspaceRoot = strings.TrimSpace(workspaceRoot)
	s.activeWorkdir = strings.TrimSpace(workdir)
}

// currentSessionStore 返回当前工作区作用域下的会话存储实例。
func (s *Service) currentSessionStore() agentsession.Store {
	workspaceRoot, _ := s.currentWorkspaceState()
	return s.sessionStoreForWorkspace(workspaceRoot)
}

// sessionStoreForWorkspace 返回指定工作区根目录下的会话存储实例。
func (s *Service) sessionStoreForWorkspace(workspaceRoot string) agentsession.Store {
	if s.storeProvider == nil {
		return nil
	}
	return s.storeProvider.StoreForWorkspace(workspaceRoot)
}

// setOperationState 记录当前正在执行的运行态操作类型。
func (s *Service) setOperationState(name string) {
	s.operationStateMu.Lock()
	defer s.operationStateMu.Unlock()
	s.activeOperation = strings.TrimSpace(name)
}

// clearOperationState 清空当前运行态操作标记。
func (s *Service) clearOperationState() {
	s.operationStateMu.Lock()
	defer s.operationStateMu.Unlock()
	s.activeOperation = ""
}

// currentOperationState 返回当前运行态操作标记。
func (s *Service) currentOperationState() string {
	s.operationStateMu.RLock()
	defer s.operationStateMu.RUnlock()
	return strings.TrimSpace(s.activeOperation)
}

// initializeWorkspaceState 以配置中的启动目录初始化当前工作区状态。
func (s *Service) initializeWorkspaceState(workdir string) {
	trimmed := strings.TrimSpace(workdir)
	if trimmed == "" {
		return
	}

	root := trimmed
	if resolvedWorkdir, resolvedRoot, err := resolveWorkspaceSelection(context.Background(), trimmed, ""); err == nil {
		trimmed = resolvedWorkdir
		root = resolvedRoot
	}
	s.setWorkspaceState(root, trimmed)
}

// SwitchWorkspace 根据请求路径更新当前工作区上下文，并决定是否重置为草稿态。
func (s *Service) SwitchWorkspace(ctx context.Context, input WorkspaceSwitchInput) (WorkspaceSwitchResult, error) {
	if err := ctx.Err(); err != nil {
		return WorkspaceSwitchResult{}, err
	}

	currentRoot, currentWorkdir := s.currentWorkspaceState()
	baseWorkdir := effectiveWorkspaceBase(currentWorkdir, currentRoot)
	result := WorkspaceSwitchResult{}

	var session agentsession.Session
	if sessionID := strings.TrimSpace(input.SessionID); sessionID != "" {
		store := s.currentSessionStore()
		if store == nil {
			return WorkspaceSwitchResult{}, errors.New("runtime: session store is nil")
		}
		loaded, err := store.Load(ctx, sessionID)
		if err != nil {
			return WorkspaceSwitchResult{}, err
		}
		session = loaded
		baseWorkdir = effectiveSessionWorkdir(session.Workdir, baseWorkdir)
	}

	workdir, workspaceRoot, err := resolveWorkspaceSelection(ctx, baseWorkdir, input.RequestedPath)
	if err != nil {
		return WorkspaceSwitchResult{}, err
	}

	workspaceChanged := !sameWorkspacePath(currentRoot, workspaceRoot)
	if workspaceChanged && s.currentOperationState() != "" {
		return WorkspaceSwitchResult{}, fmt.Errorf("runtime: cannot switch workspace while %s is running", s.currentOperationState())
	}

	result.Workdir = workdir
	result.WorkspaceRoot = workspaceRoot
	result.WorkspaceChanged = workspaceChanged

	if workspaceChanged {
		s.setWorkspaceState(workspaceRoot, workdir)
		result.ResetToDraft = true
		return result, nil
	}

	if strings.TrimSpace(session.ID) == "" {
		s.setWorkspaceState(workspaceRoot, workdir)
		return result, nil
	}

	result.KeepSession = true
	if session.Workdir == workdir {
		s.setWorkspaceState(workspaceRoot, workdir)
		result.Session = session
		return result, nil
	}

	store := s.sessionStoreForWorkspace(workspaceRoot)
	if store == nil {
		return WorkspaceSwitchResult{}, errors.New("runtime: session store is nil")
	}
	session.Workdir = workdir
	session.UpdatedAt = time.Now()
	if err := store.Save(ctx, &session); err != nil {
		return WorkspaceSwitchResult{}, err
	}
	s.setWorkspaceState(workspaceRoot, workdir)
	result.Session = session
	return result, nil
}

// SetSessionWorkdir 兼容旧测试入口，并复用新的工作区切换语义。
func (s *Service) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return agentsession.Session{}, errors.New("runtime: session id is empty")
	}

	result, err := s.SwitchWorkspace(ctx, WorkspaceSwitchInput{
		SessionID:     sessionID,
		RequestedPath: workdir,
	})
	if err != nil {
		return agentsession.Session{}, err
	}
	if result.ResetToDraft {
		return agentsession.Session{}, errors.New("runtime: workspace switch reset current session")
	}
	return result.Session, nil
}
