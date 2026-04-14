//go:build windows

package transport

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func TestListenNamedPipeAcceptsConnection(t *testing.T) {
	pipePath := fmt.Sprintf(`\\.\pipe\neocode-gateway-test-%d`, time.Now().UnixNano())
	listener, err := Listen(pipePath)
	if err != nil {
		t.Fatalf("listen named pipe: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	acceptDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			acceptDone <- acceptErr
			return
		}
		_ = conn.Close()
		acceptDone <- nil
	}()

	timeout := 2 * time.Second
	conn, err := winio.DialPipe(pipePath, &timeout)
	if err != nil {
		t.Fatalf("dial named pipe: %v", err)
	}
	_ = conn.Close()

	select {
	case acceptErr := <-acceptDone:
		if acceptErr != nil {
			t.Fatalf("accept connection: %v", acceptErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("accept timed out")
	}
}

func TestNewRestrictedPipeConfigContainsExpectedSIDs(t *testing.T) {
	config, err := newRestrictedPipeConfig()
	if err != nil {
		t.Fatalf("new restricted pipe config: %v", err)
	}
	if config == nil {
		t.Fatal("pipe config is nil")
	}
	if config.SecurityDescriptor == "" {
		t.Fatal("security descriptor is empty")
	}

	currentUserSID, err := currentProcessUserSID()
	if err != nil {
		t.Fatalf("current user sid: %v", err)
	}
	if !strings.Contains(config.SecurityDescriptor, currentUserSID) {
		t.Fatalf("security descriptor does not contain current user sid %q", currentUserSID)
	}

	systemSID, err := wellKnownSIDString(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatalf("system sid: %v", err)
	}
	if !strings.Contains(config.SecurityDescriptor, systemSID) {
		t.Fatalf("security descriptor does not contain system sid %q", systemSID)
	}

	adminSID, err := wellKnownSIDString(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatalf("administrators sid: %v", err)
	}
	if !strings.Contains(config.SecurityDescriptor, adminSID) {
		t.Fatalf("security descriptor does not contain administrators sid %q", adminSID)
	}
}
