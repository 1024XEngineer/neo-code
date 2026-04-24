package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

var (
	atomicCreateTemp = os.CreateTemp
	atomicRename     = os.Rename
	atomicGOOS       = runtime.GOOS
)

// writeFileAtomically 通过同目录临时文件与原子替换写入目标文件。
func writeFileAtomically(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	pattern := "." + filepath.Base(path) + ".tmp-*"
	tempFile, err := atomicCreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tempPath, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := atomicRename(tempPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	cleanupTemp = false

	if err := fsyncDirectory(dir); err != nil {
		return fmt.Errorf("sync target directory: %w", err)
	}
	return nil
}

// fsyncDirectory 尝试同步目录元数据，确保 rename 后的目录项在支持的平台尽快落盘。
func fsyncDirectory(dir string) error {
	if atomicGOOS == "windows" {
		return nil
	}
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil && !isBestEffortDirectorySyncError(err) {
		return err
	}
	return nil
}

// isBestEffortDirectorySyncError 判断目录 fsync 是否因为平台或文件系统限制而允许退化为 best-effort。
func isBestEffortDirectorySyncError(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, os.ErrInvalid)
}
