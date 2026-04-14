//go:build windows

package transport

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const (
	pipeSDDLDiscretionaryACL = "D:P"
)

var (
	listenPipeFn             = winio.ListenPipe
	currentProcessUserSIDFn  = currentProcessUserSID
	wellKnownSIDStringFn     = wellKnownSIDString
	getCurrentProcessTokenFn = windows.GetCurrentProcessToken
	createWellKnownSIDFn     = windows.CreateWellKnownSid
)

// Listen 在 Windows 系统上启动 Named Pipe 监听，并显式收敛访问控制。
func Listen(address string) (net.Listener, error) {
	config, err := newRestrictedPipeConfig()
	if err != nil {
		return nil, err
	}

	listener, err := listenPipeFn(address, config)
	if err != nil {
		return nil, fmt.Errorf("gateway: listen named pipe: %w", err)
	}
	return newCleanupListener(listener, nil), nil
}

// newRestrictedPipeConfig 构建最小权限 PipeConfig，仅允许 SYSTEM、管理员组与当前用户访问。
func newRestrictedPipeConfig() (*winio.PipeConfig, error) {
	securityDescriptor, err := buildRestrictedPipeSecurityDescriptor()
	if err != nil {
		return nil, err
	}
	return &winio.PipeConfig{SecurityDescriptor: securityDescriptor}, nil
}

// buildRestrictedPipeSecurityDescriptor 生成管道 ACL 的 SDDL 表达式。
func buildRestrictedPipeSecurityDescriptor() (string, error) {
	currentUserSID, err := currentProcessUserSIDFn()
	if err != nil {
		return "", err
	}

	systemSID, err := wellKnownSIDStringFn(windows.WinLocalSystemSid)
	if err != nil {
		return "", fmt.Errorf("gateway: resolve local-system sid: %w", err)
	}

	administratorsSID, err := wellKnownSIDStringFn(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return "", fmt.Errorf("gateway: resolve administrators sid: %w", err)
	}

	return fmt.Sprintf(
		"%s(%s)(%s)(%s)",
		pipeSDDLDiscretionaryACL,
		allowGenericAllAce(systemSID),
		allowGenericAllAce(administratorsSID),
		allowGenericAllAce(currentUserSID),
	), nil
}

// currentProcessUserSID 返回当前进程用户的 SID 字符串。
func currentProcessUserSID() (string, error) {
	tokenUser, err := getCurrentProcessTokenFn().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("gateway: query current token user: %w", err)
	}
	if tokenUser == nil || tokenUser.User.Sid == nil {
		return "", fmt.Errorf("gateway: current token user sid is empty")
	}
	return tokenUser.User.Sid.String(), nil
}

// wellKnownSIDString 将系统内置 SID 类型转换为 SID 字符串。
func wellKnownSIDString(sidType windows.WELL_KNOWN_SID_TYPE) (string, error) {
	sid, err := createWellKnownSIDFn(sidType)
	if err != nil {
		return "", err
	}
	return sid.String(), nil
}

// allowGenericAllAce 为指定 SID 生成“完全控制”ACE。
func allowGenericAllAce(sid string) string {
	return fmt.Sprintf("A;;GA;;;%s", sid)
}
