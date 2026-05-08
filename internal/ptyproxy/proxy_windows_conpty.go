//go:build windows

package ptyproxy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

const (
	windowsConPTYResizeInterval = 500 * time.Millisecond
	windowsConPTYDefaultColumns = int16(80)
	windowsConPTYDefaultRows    = int16(25)
	windowsConPTYInt16Max       = int16(32767)
)

var (
	windowsCreatePseudoConsole = windows.CreatePseudoConsole
	windowsResizePseudoConsole = windows.ResizePseudoConsole
	windowsClosePseudoConsole  = windows.ClosePseudoConsole
	windowsCreatePipe          = windows.CreatePipe
	windowsGetConsoleMode      = windows.GetConsoleMode
	windowsGetConsoleInfo      = windows.GetConsoleScreenBufferInfo
	windowsWriteConsole        = windows.WriteConsole
)

// windowsConPTYShell 持有 ConPTY 进程与输入输出管道句柄。
type windowsConPTYShell struct {
	console      windows.Handle
	process      windows.Handle
	thread       windows.Handle
	inputWriter  *os.File
	outputReader *os.File

	closeOnce sync.Once
}

// startWindowsConPTYShell 创建并启动绑定 ConPTY 的 shell 进程。
func startWindowsConPTYShell(shellPath string, shellArgs []string, workdir string, env []string) (*windowsConPTYShell, error) {
	normalizedShellPath := strings.TrimSpace(shellPath)
	if normalizedShellPath == "" {
		return nil, errors.New("shell executable is empty")
	}

	conPTYSize := resolveWindowsConPTYSize()
	inReaderHandle, inWriterHandle, err := createWindowsInheritablePipeHandles()
	if err != nil {
		return nil, fmt.Errorf("create conpty input pipe: %w", err)
	}
	outReaderHandle, outWriterHandle, err := createWindowsInheritablePipeHandles()
	if err != nil {
		closeWindowsHandle(&inReaderHandle)
		closeWindowsHandle(&inWriterHandle)
		return nil, fmt.Errorf("create conpty output pipe: %w", err)
	}

	var console windows.Handle
	if err := windowsCreatePseudoConsole(
		conPTYSize,
		inReaderHandle,
		outWriterHandle,
		0,
		&console,
	); err != nil {
		closeWindowsHandle(&inReaderHandle)
		closeWindowsHandle(&inWriterHandle)
		closeWindowsHandle(&outReaderHandle)
		closeWindowsHandle(&outWriterHandle)
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}

	attributeList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windowsClosePseudoConsole(console)
		closeWindowsHandle(&inReaderHandle)
		closeWindowsHandle(&inWriterHandle)
		closeWindowsHandle(&outReaderHandle)
		closeWindowsHandle(&outWriterHandle)
		return nil, fmt.Errorf("new proc thread attribute list: %w", err)
	}
	defer attributeList.Delete()

	if err := attributeList.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		unsafe.Pointer(console),
		unsafe.Sizeof(console),
	); err != nil {
		windowsClosePseudoConsole(console)
		closeWindowsHandle(&inReaderHandle)
		closeWindowsHandle(&inWriterHandle)
		closeWindowsHandle(&outReaderHandle)
		closeWindowsHandle(&outWriterHandle)
		return nil, fmt.Errorf("update pseudo console attribute: %w", err)
	}

	startupInfo := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
		},
		ProcThreadAttributeList: attributeList.List(),
	}

	commandLine, err := windows.UTF16PtrFromString(
		windows.ComposeCommandLine(append([]string{normalizedShellPath}, shellArgs...)),
	)
	if err != nil {
		windowsClosePseudoConsole(console)
		closeWindowsHandle(&inReaderHandle)
		closeWindowsHandle(&inWriterHandle)
		closeWindowsHandle(&outReaderHandle)
		closeWindowsHandle(&outWriterHandle)
		return nil, fmt.Errorf("compose shell command line: %w", err)
	}

	var workdirPtr *uint16
	normalizedWorkdir := strings.TrimSpace(workdir)
	if normalizedWorkdir != "" {
		workdirPtr, err = windows.UTF16PtrFromString(normalizedWorkdir)
		if err != nil {
			windowsClosePseudoConsole(console)
			closeWindowsHandle(&inReaderHandle)
			closeWindowsHandle(&inWriterHandle)
			closeWindowsHandle(&outReaderHandle)
			closeWindowsHandle(&outWriterHandle)
			return nil, fmt.Errorf("encode shell workdir: %w", err)
		}
	}

	environmentBlock, err := buildWindowsEnvironmentBlock(env)
	if err != nil {
		windowsClosePseudoConsole(console)
		closeWindowsHandle(&inReaderHandle)
		closeWindowsHandle(&inWriterHandle)
		closeWindowsHandle(&outReaderHandle)
		closeWindowsHandle(&outWriterHandle)
		return nil, fmt.Errorf("build shell environment block: %w", err)
	}
	var environmentPtr *uint16
	if len(environmentBlock) > 0 {
		environmentPtr = &environmentBlock[0]
	}

	var processInfo windows.ProcessInformation
	if err := windows.CreateProcess(
		nil,
		commandLine,
		nil,
		nil,
		false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		environmentPtr,
		workdirPtr,
		&startupInfo.StartupInfo,
		&processInfo,
	); err != nil {
		windowsClosePseudoConsole(console)
		closeWindowsHandle(&inReaderHandle)
		closeWindowsHandle(&inWriterHandle)
		closeWindowsHandle(&outReaderHandle)
		closeWindowsHandle(&outWriterHandle)
		return nil, fmt.Errorf("create conpty process: %w", err)
	}

	// CreateProcess 成功后，父进程不再需要 ConPTY 侧的 read/write 端句柄。
	closeWindowsHandle(&inReaderHandle)
	closeWindowsHandle(&outWriterHandle)

	inputWriter := os.NewFile(uintptr(inWriterHandle), "conpty-input-writer")
	if inputWriter == nil {
		windowsClosePseudoConsole(console)
		closeWindowsHandle(&inWriterHandle)
		closeWindowsHandle(&outReaderHandle)
		_ = windows.CloseHandle(processInfo.Process)
		_ = windows.CloseHandle(processInfo.Thread)
		return nil, errors.New("create input writer file from conpty pipe handle")
	}
	inWriterHandle = 0

	outputReader := os.NewFile(uintptr(outReaderHandle), "conpty-output-reader")
	if outputReader == nil {
		_ = inputWriter.Close()
		windowsClosePseudoConsole(console)
		closeWindowsHandle(&outReaderHandle)
		_ = windows.CloseHandle(processInfo.Process)
		_ = windows.CloseHandle(processInfo.Thread)
		return nil, errors.New("create output reader file from conpty pipe handle")
	}
	outReaderHandle = 0

	return &windowsConPTYShell{
		console:      console,
		process:      processInfo.Process,
		thread:       processInfo.Thread,
		inputWriter:  inputWriter,
		outputReader: outputReader,
	}, nil
}

// createWindowsInheritablePipeHandles 负责创建可继承的匿名管道句柄，满足 ConPTY 的句柄要求。
func createWindowsInheritablePipeHandles() (windows.Handle, windows.Handle, error) {
	securityAttributes := windows.SecurityAttributes{
		Length:        uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		InheritHandle: 1,
	}
	var reader windows.Handle
	var writer windows.Handle
	if err := windowsCreatePipe(&reader, &writer, &securityAttributes, 0); err != nil {
		return 0, 0, err
	}
	return reader, writer, nil
}

// closeWindowsHandle 在句柄非空时安全关闭并清零，避免重复关闭带来的副作用。
func closeWindowsHandle(handle *windows.Handle) {
	if handle == nil || *handle == 0 {
		return
	}
	_ = windows.CloseHandle(*handle)
	*handle = 0
}

// InputWriter 返回写入 ConPTY 输入端的 writer。
func (s *windowsConPTYShell) InputWriter() io.Writer {
	if s == nil {
		return nil
	}
	return s.inputWriter
}

// OutputReader 返回读取 ConPTY 输出端的 reader。
func (s *windowsConPTYShell) OutputReader() io.Reader {
	if s == nil {
		return nil
	}
	return s.outputReader
}

// Wait 等待 ConPTY shell 进程退出，并返回退出状态错误。
func (s *windowsConPTYShell) Wait() error {
	if s == nil || s.process == 0 {
		return nil
	}
	status, err := windows.WaitForSingleObject(s.process, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("wait conpty process: %w", err)
	}
	if status != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("wait conpty process returned status: %d", status)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(s.process, &exitCode); err != nil {
		return fmt.Errorf("query conpty process exit code: %w", err)
	}
	if exitCode == 0 {
		return nil
	}
	return windowsShellExitError{Code: exitCode}
}

type windowsShellExitError struct {
	Code uint32
}

// Error 返回 shell 进程退出码，用于上层区分启动失败和用户退出交互 shell。
func (e windowsShellExitError) Error() string {
	return fmt.Sprintf("ptyproxy: shell exited with code %d", e.Code)
}

// Terminate 主动结束 ConPTY shell 进程。
func (s *windowsConPTYShell) Terminate() error {
	if s == nil || s.process == 0 {
		return nil
	}
	if err := windows.TerminateProcess(s.process, 1); err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_INVALID_HANDLE) {
			return nil
		}
		return fmt.Errorf("terminate conpty process: %w", err)
	}
	return nil
}

// Resize 调整 ConPTY 的终端尺寸。
func (s *windowsConPTYShell) Resize(columns int16, rows int16) error {
	if s == nil || s.console == 0 {
		return nil
	}
	if columns <= 0 || rows <= 0 {
		return nil
	}
	return windowsResizePseudoConsole(s.console, windows.Coord{X: columns, Y: rows})
}

// WriteScreenText 向 ConPTY 内部屏幕缓冲区写入文本，避免经过 shell stdin 被当作命令执行。
func (s *windowsConPTYShell) WriteScreenText(text string) error {
	if s == nil || s.console == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	utf16Text, err := windows.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("encode conpty screen text: %w", err)
	}
	if len(utf16Text) <= 1 {
		return nil
	}
	var written uint32
	if err := windowsWriteConsole(
		s.console,
		&utf16Text[0],
		uint32(len(utf16Text)-1),
		&written,
		nil,
	); err != nil {
		return fmt.Errorf("write conpty screen text: %w", err)
	}
	return nil
}

// CloseOutputReader 关闭 ConPTY 输出读取端，释放读取协程阻塞。
func (s *windowsConPTYShell) CloseOutputReader() error {
	if s == nil || s.outputReader == nil {
		return nil
	}
	err := s.outputReader.Close()
	s.outputReader = nil
	return err
}

// Close 释放 ConPTY 相关句柄与管道资源。
func (s *windowsConPTYShell) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	s.closeOnce.Do(func() {
		if s.thread != 0 {
			closeErr = errors.Join(closeErr, windows.CloseHandle(s.thread))
			s.thread = 0
		}
		if s.process != 0 {
			closeErr = errors.Join(closeErr, windows.CloseHandle(s.process))
			s.process = 0
		}
		if s.console != 0 {
			windowsClosePseudoConsole(s.console)
			s.console = 0
		}
		if s.inputWriter != nil {
			closeErr = errors.Join(closeErr, s.inputWriter.Close())
			s.inputWriter = nil
		}
		if s.outputReader != nil {
			closeErr = errors.Join(closeErr, s.outputReader.Close())
			s.outputReader = nil
		}
	})
	return closeErr
}

// watchWindowsConPTYResize 轮询宿主终端尺寸并同步到 ConPTY。
func watchWindowsConPTYResize(shell *windowsConPTYShell, errWriter io.Writer) func() {
	if shell == nil {
		return func() {}
	}

	initialSize := resolveWindowsConPTYSize()
	columns, rows := initialSize.X, initialSize.Y
	_ = shell.Resize(columns, rows)

	stopCh := make(chan struct{})
	var stopOnce sync.Once
	var watcherWG sync.WaitGroup
	watcherWG.Add(1)
	go func() {
		defer watcherWG.Done()
		ticker := time.NewTicker(windowsConPTYResizeInterval)
		defer ticker.Stop()

		lastColumns := columns
		lastRows := rows
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				size := resolveWindowsConPTYSize()
				if size.X == lastColumns && size.Y == lastRows {
					continue
				}
				if err := shell.Resize(size.X, size.Y); err != nil && errWriter != nil {
					writeProxyf(errWriter, "neocode shell: resize conpty failed: %v\n", err)
				}
				lastColumns = size.X
				lastRows = size.Y
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			close(stopCh)
			watcherWG.Wait()
		})
	}
}

// resolveWindowsConPTYSize 解析宿主终端大小，失败时回退默认值。
func resolveWindowsConPTYSize() windows.Coord {
	columns := windowsConPTYDefaultColumns
	rows := windowsConPTYDefaultRows

	resolveFromConsole := func(file *os.File) (windows.Coord, bool) {
		if file == nil {
			return windows.Coord{}, false
		}
		handle := windows.Handle(file.Fd())
		var mode uint32
		if err := windowsGetConsoleMode(handle, &mode); err != nil {
			return windows.Coord{}, false
		}
		var info windows.ConsoleScreenBufferInfo
		if err := windowsGetConsoleInfo(handle, &info); err != nil {
			return windows.Coord{}, false
		}
		width := int(info.Window.Right-info.Window.Left) + 1
		height := int(info.Window.Bottom-info.Window.Top) + 1
		if width <= 0 || height <= 0 {
			return windows.Coord{}, false
		}
		return windows.Coord{
			X: clampWindowsConPTYSize(width),
			Y: clampWindowsConPTYSize(height),
		}, true
	}

	for _, file := range []*os.File{os.Stdout, os.Stderr} {
		if size, ok := resolveFromConsole(file); ok {
			return size
		}
	}

	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return windows.Coord{X: columns, Y: rows}
	}
	if width > 0 {
		columns = clampWindowsConPTYSize(width)
	}
	if height > 0 {
		rows = clampWindowsConPTYSize(height)
	}
	if columns <= 0 {
		columns = windowsConPTYDefaultColumns
	}
	if rows <= 0 {
		rows = windowsConPTYDefaultRows
	}
	return windows.Coord{X: columns, Y: rows}
}

// clampWindowsConPTYSize 将整数尺寸限制到 ConPTY 支持的正向 int16 范围。
func clampWindowsConPTYSize(value int) int16 {
	if value <= 0 {
		return 0
	}
	if value > int(windowsConPTYInt16Max) {
		return windowsConPTYInt16Max
	}
	return int16(value)
}

// buildWindowsEnvironmentBlock 构建 CreateProcess 所需的 UTF-16 环境块。
func buildWindowsEnvironmentBlock(environment []string) ([]uint16, error) {
	if len(environment) == 0 {
		return []uint16{0, 0}, nil
	}
	entries := append([]string(nil), environment...)
	sort.Slice(entries, func(i int, j int) bool {
		leftKey := windowsEnvironmentKey(entries[i])
		rightKey := windowsEnvironmentKey(entries[j])
		if leftKey == rightKey {
			return entries[i] < entries[j]
		}
		return leftKey < rightKey
	})

	block := make([]uint16, 0, len(entries)*8)
	for _, entry := range entries {
		if strings.ContainsRune(entry, rune(0)) {
			return nil, fmt.Errorf("environment contains NUL character")
		}
		block = append(block, utf16.Encode([]rune(entry))...)
		block = append(block, 0)
	}
	block = append(block, 0)
	return block, nil
}

// windowsEnvironmentKey 提取环境变量键并按大小写无关方式归一化。
func windowsEnvironmentKey(entry string) string {
	index := strings.Index(entry, "=")
	if index < 0 {
		return strings.ToUpper(strings.TrimSpace(entry))
	}
	return strings.ToUpper(strings.TrimSpace(entry[:index]))
}
