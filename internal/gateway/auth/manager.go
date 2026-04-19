package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultAuthRelativePath 定义默认凭证文件相对路径。
	DefaultAuthRelativePath = ".neocode/auth.json"
	DefaultLocalSubjectID   = "local_admin"
	// credentialSchemaVersion 定义凭证文件结构版本号。
	credentialSchemaVersion = 1
	// tokenRandomByteLength 定义静默认证 Token 的随机字节长度。
	tokenRandomByteLength = 32
)

const (
	authDirPerm  = 0o700
	authFilePerm = 0o600
)

const (
	authTempFilePattern = ".auth.json.tmp-*"
)

var (
	errUnsafeCredentialPath  = errors.New("unsafe credential path")
	errInvalidCredentialBody = errors.New("invalid credential body")
)

// Credentials 表示持久化在磁盘上的认证凭证结构。
type Credentials struct {
	Version   int       `json:"version"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Manager 负责加载或生成本地静默认证 Token，并提供校验能力。
type Manager struct {
	path        string
	credentials Credentials
}

// NewManager 创建并初始化认证管理器；若凭证文件不存在或无效则自动重建。
func NewManager(path string) (*Manager, error) {
	resolvedPath, err := resolveAuthPath(path)
	if err != nil {
		return nil, err
	}

	manager := &Manager{
		path: resolvedPath,
	}
	if loadErr := manager.loadOrCreate(); loadErr != nil {
		return nil, loadErr
	}
	return manager, nil
}

// Path 返回认证凭证文件路径。
func (m *Manager) Path() string {
	if m == nil {
		return ""
	}
	return m.path
}

// Token 返回当前有效 Token。
func (m *Manager) Token() string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.credentials.Token)
}

// ValidateToken 校验输入 Token 是否与本地凭证一致。
func (m *Manager) ValidateToken(token string) bool {
	if m == nil {
		return false
	}
	return strings.TrimSpace(token) != "" && strings.TrimSpace(token) == strings.TrimSpace(m.credentials.Token)
}

// ResolveSubjectID 鍦?token 閫氳繃鏍￠獙鏃惰繑鍥炵ǔ瀹氱殑 subject_id銆?
func (m *Manager) ResolveSubjectID(token string) (string, bool) {
	if !m.ValidateToken(token) {
		return "", false
	}
	return DefaultLocalSubjectID, true
}

// LoadTokenFromFile 从指定路径读取静默认证 Token。
func LoadTokenFromFile(path string) (string, error) {
	resolvedPath, err := resolveAuthPath(path)
	if err != nil {
		return "", err
	}
	credentials, err := readCredentials(resolvedPath)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(credentials.Token)
	if token == "" {
		return "", fmt.Errorf("gateway auth: token is empty in %s", resolvedPath)
	}
	return token, nil
}

// DefaultAuthPath 返回默认认证文件路径。
func DefaultAuthPath() (string, error) {
	return resolveAuthPath("")
}

// loadOrCreate 加载现有凭证，若不存在或内容无效则自动重建。
func (m *Manager) loadOrCreate() error {
	if m == nil {
		return fmt.Errorf("gateway auth: manager is nil")
	}

	if err := ensureAuthDir(filepath.Dir(m.path)); err != nil {
		return err
	}

	credentials, readErr := readCredentials(m.path)
	if readErr == nil && isValidCredentials(credentials) {
		m.credentials = credentials
		return nil
	}
	if readErr != nil && !isRecoverableCredentialReadError(readErr) {
		return readErr
	}

	createdCredentials, createErr := buildCredentials(time.Now().UTC())
	if createErr != nil {
		return createErr
	}
	if writeErr := writeCredentials(m.path, createdCredentials); writeErr != nil {
		return writeErr
	}
	m.credentials = createdCredentials
	return nil
}

// resolveAuthPath 解析认证文件路径并清理空白。
func resolveAuthPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed != "" {
		absolutePath, err := filepath.Abs(filepath.Clean(trimmed))
		if err != nil {
			return "", fmt.Errorf("gateway auth: resolve auth path: %w", err)
		}
		return absolutePath, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("gateway auth: resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, DefaultAuthRelativePath), nil
}

// ensureAuthDir 确保认证目录存在并在 Unix 上收紧目录权限。
func ensureAuthDir(dir string) error {
	if err := os.MkdirAll(dir, authDirPerm); err != nil {
		return fmt.Errorf("gateway auth: create auth dir: %w", err)
	}
	if err := ensureSafeCredentialDirectory(dir); err != nil {
		return err
	}
	if err := applyAuthDirPermission(dir); err != nil {
		return err
	}
	return nil
}

// readCredentials 读取并解析认证凭证文件。
func readCredentials(path string) (Credentials, error) {
	if err := ensureSafeCredentialFilePath(path, false); err != nil {
		return Credentials{}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("gateway auth: read auth file: %w", err)
	}

	var credentials Credentials
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("gateway auth: decode auth file: %w: %w", errInvalidCredentialBody, err)
	}
	return credentials, nil
}

// buildCredentials 生成新的认证凭证结构。
func buildCredentials(now time.Time) (Credentials, error) {
	token, err := generateToken()
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{
		Version:   credentialSchemaVersion,
		Token:     token,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// generateToken 生成高强度随机 Token。
func generateToken() (string, error) {
	seed := make([]byte, tokenRandomByteLength)
	if _, err := rand.Read(seed); err != nil {
		return "", fmt.Errorf("gateway auth: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(seed), nil
}

// writeCredentials 持久化凭证文件并在 Unix 上收紧文件权限。
func writeCredentials(path string, credentials Credentials) error {
	raw, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("gateway auth: encode credentials: %w", err)
	}
	raw = append(raw, '\n')
	if err := ensureSafeCredentialFilePath(path, true); err != nil {
		return err
	}

	authDir := filepath.Dir(path)
	if err := ensureSafeCredentialDirectory(authDir); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(authDir, authTempFilePattern)
	if err != nil {
		return fmt.Errorf("gateway auth: create temp auth file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(raw); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("gateway auth: write temp auth file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("gateway auth: sync temp auth file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("gateway auth: close temp auth file: %w", err)
	}

	if err := applyAuthFilePermission(tempPath); err != nil {
		return err
	}
	if err := ensureSafeCredentialFilePath(path, true); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("gateway auth: replace auth file atomically: %w", err)
	}
	cleanupTemp = false

	if err := applyAuthFilePermission(path); err != nil {
		return err
	}
	return nil
}

// isValidCredentials 判断凭证内容是否完整可用。
func isValidCredentials(credentials Credentials) bool {
	return credentials.Version >= credentialSchemaVersion && strings.TrimSpace(credentials.Token) != ""
}

// isRecoverableCredentialReadError 判断读取凭证失败是否允许走自动重建流程。
func isRecoverableCredentialReadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return errors.Is(err, errInvalidCredentialBody)
}

// ensureSafeCredentialDirectory 校验凭证目录不是链接路径，避免目录级别劫持。
func ensureSafeCredentialDirectory(dir string) error {
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("gateway auth: inspect auth dir: %w", err)
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("gateway auth: auth dir is symbolic link: %w", errUnsafeCredentialPath)
	}
	return nil
}

// ensureSafeCredentialFilePath 校验凭证文件路径不为软链接/危险硬链接。
func ensureSafeCredentialFilePath(path string, allowNotExist bool) error {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		if allowNotExist && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("gateway auth: inspect auth file: %w", err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("gateway auth: auth file is symbolic link: %w", errUnsafeCredentialPath)
	}
	if isUnsafeCredentialHardLink(fileInfo) {
		return fmt.Errorf("gateway auth: auth file is hard link: %w", errUnsafeCredentialPath)
	}
	return nil
}
