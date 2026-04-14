//go:build windows

package transport

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func TestDefaultListenAddressWindows(t *testing.T) {
	address, err := DefaultListenAddress()
	if err != nil {
		t.Fatalf("default listen address: %v", err)
	}
	if address != defaultWindowsNamedPipePath {
		t.Fatalf("default address = %q, want %q", address, defaultWindowsNamedPipePath)
	}
}

func TestNewCleanupListenerBranches(t *testing.T) {
	base := &stubNetListener{}
	if got := newCleanupListener(base, nil); got != base {
		t.Fatal("expected original listener when cleanup is nil")
	}

	closeErr := errors.New("close failed")
	cleanupErr := errors.New("cleanup failed")
	wrapped := newCleanupListener(&stubNetListener{closeErr: closeErr}, func() error { return cleanupErr })
	if err := wrapped.Close(); err == nil {
		t.Fatal("expected joined error")
	} else {
		if !errors.Is(err, closeErr) {
			t.Fatalf("joined error should include close error, got %v", err)
		}
		if !errors.Is(err, cleanupErr) {
			t.Fatalf("joined error should include cleanup error, got %v", err)
		}
	}
}

func TestBuildRestrictedPipeSecurityDescriptorContainsExpectedACEs(t *testing.T) {
	sddl, err := buildRestrictedPipeSecurityDescriptor()
	if err != nil {
		t.Fatalf("build restricted descriptor: %v", err)
	}
	if !strings.HasPrefix(sddl, pipeSDDLDiscretionaryACL) {
		t.Fatalf("sddl prefix = %q, want starts with %q", sddl, pipeSDDLDiscretionaryACL)
	}
	if strings.Count(sddl, "A;;GA;;;") != 3 {
		t.Fatalf("sddl should contain 3 allow full-control ACE entries, got %q", sddl)
	}
}

func TestNewRestrictedPipeConfigErrorBranch(t *testing.T) {
	originalCurrent := currentProcessUserSIDFn
	currentProcessUserSIDFn = func() (string, error) {
		return "", errors.New("current user failed")
	}
	t.Cleanup(func() {
		currentProcessUserSIDFn = originalCurrent
	})

	_, err := newRestrictedPipeConfig()
	if err == nil || !strings.Contains(err.Error(), "current user failed") {
		t.Fatalf("expected current user error, got %v", err)
	}
}

func TestListenReturnsConfigError(t *testing.T) {
	originalCurrent := currentProcessUserSIDFn
	currentProcessUserSIDFn = func() (string, error) {
		return "", errors.New("restricted config failed")
	}
	t.Cleanup(func() {
		currentProcessUserSIDFn = originalCurrent
	})

	_, err := Listen(`\\.\pipe\neocode-gateway-config-error-test`)
	if err == nil || !strings.Contains(err.Error(), "restricted config failed") {
		t.Fatalf("expected config build failure, got %v", err)
	}
}

func TestBuildRestrictedPipeSecurityDescriptorSystemErrorBranch(t *testing.T) {
	originalCurrent := currentProcessUserSIDFn
	originalWellKnown := wellKnownSIDStringFn
	currentProcessUserSIDFn = func() (string, error) { return "S-1-5-21-1", nil }
	wellKnownSIDStringFn = func(sidType windows.WELL_KNOWN_SID_TYPE) (string, error) {
		if sidType == windows.WinLocalSystemSid {
			return "", errors.New("system sid failed")
		}
		return "S-1-5-32-544", nil
	}
	t.Cleanup(func() {
		currentProcessUserSIDFn = originalCurrent
		wellKnownSIDStringFn = originalWellKnown
	})

	_, err := buildRestrictedPipeSecurityDescriptor()
	if err == nil || !strings.Contains(err.Error(), "system sid failed") {
		t.Fatalf("expected system sid error, got %v", err)
	}
}

func TestBuildRestrictedPipeSecurityDescriptorAdminErrorBranch(t *testing.T) {
	originalCurrent := currentProcessUserSIDFn
	originalWellKnown := wellKnownSIDStringFn
	currentProcessUserSIDFn = func() (string, error) { return "S-1-5-21-1", nil }
	wellKnownSIDStringFn = func(sidType windows.WELL_KNOWN_SID_TYPE) (string, error) {
		if sidType == windows.WinBuiltinAdministratorsSid {
			return "", errors.New("admin sid failed")
		}
		return "S-1-5-18", nil
	}
	t.Cleanup(func() {
		currentProcessUserSIDFn = originalCurrent
		wellKnownSIDStringFn = originalWellKnown
	})

	_, err := buildRestrictedPipeSecurityDescriptor()
	if err == nil || !strings.Contains(err.Error(), "admin sid failed") {
		t.Fatalf("expected admin sid error, got %v", err)
	}
}

func TestListenReturnsListenPipeError(t *testing.T) {
	originalCurrent := currentProcessUserSIDFn
	originalWellKnown := wellKnownSIDStringFn
	originalListenPipe := listenPipeFn
	currentProcessUserSIDFn = func() (string, error) { return "S-1-5-21-1", nil }
	wellKnownSIDStringFn = func(sidType windows.WELL_KNOWN_SID_TYPE) (string, error) {
		if sidType == windows.WinLocalSystemSid {
			return "S-1-5-18", nil
		}
		if sidType == windows.WinBuiltinAdministratorsSid {
			return "S-1-5-32-544", nil
		}
		return "", errors.New("unexpected sid type")
	}
	listenPipeFn = func(_ string, _ *winio.PipeConfig) (net.Listener, error) {
		return nil, errors.New("listen pipe failed")
	}
	t.Cleanup(func() {
		currentProcessUserSIDFn = originalCurrent
		wellKnownSIDStringFn = originalWellKnown
		listenPipeFn = originalListenPipe
	})

	_, err := Listen(`\\.\pipe\neocode-gateway-error-test`)
	if err == nil || !strings.Contains(err.Error(), "listen pipe failed") {
		t.Fatalf("expected listen pipe failure, got %v", err)
	}
}

func TestCurrentProcessUserSIDErrorBranch(t *testing.T) {
	originalTokenFn := getCurrentProcessTokenFn
	getCurrentProcessTokenFn = func() windows.Token {
		return windows.Token(0)
	}
	t.Cleanup(func() {
		getCurrentProcessTokenFn = originalTokenFn
	})

	_, err := currentProcessUserSID()
	if err == nil {
		t.Fatal("expected current process token user error")
	}
}

func TestWellKnownSIDStringErrorBranch(t *testing.T) {
	originalCreateWellKnownSID := createWellKnownSIDFn
	createWellKnownSIDFn = func(_ windows.WELL_KNOWN_SID_TYPE) (*windows.SID, error) {
		return nil, errors.New("create sid failed")
	}
	t.Cleanup(func() {
		createWellKnownSIDFn = originalCreateWellKnownSID
	})

	_, err := wellKnownSIDString(windows.WinLocalSystemSid)
	if err == nil || !strings.Contains(err.Error(), "create sid failed") {
		t.Fatalf("expected create sid failure, got %v", err)
	}
}

type stubNetListener struct {
	closeErr error
}

func (l *stubNetListener) Accept() (net.Conn, error) {
	return nil, net.ErrClosed
}

func (l *stubNetListener) Close() error {
	return l.closeErr
}

func (l *stubNetListener) Addr() net.Addr {
	return pipeAddr("stub")
}

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }
