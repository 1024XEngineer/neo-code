package checkpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	fingerprintHeadBytes = 4 * 1024
	fingerprintHashLen   = 16
)

// FileFingerprint 描述单个文件的廉价指纹。
type FileFingerprint struct {
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	HeadHash string    `json:"head_hash"`
}

// WorkdirFingerprint 是 workdir 相对路径 → 指纹的快照。
type WorkdirFingerprint map[string]FileFingerprint

// FingerprintOptions 控制扫描范围与跳过规则。
type FingerprintOptions struct {
	SkipDirs []string
	SkipExts []string
	MaxFiles int
}

// FingerprintDiff 描述两次指纹快照之间的差异。
type FingerprintDiff struct {
	Added    []string
	Deleted  []string
	Modified []string
}

// DefaultFingerprintOptions 返回常用的扫描跳过规则与上限。
func DefaultFingerprintOptions() FingerprintOptions {
	return FingerprintOptions{
		SkipDirs: []string{".git", ".neocode", ".shadow", "node_modules", ".idea", ".vscode", "vendor", "target", "dist", "build"},
		SkipExts: []string{".exe", ".dll", ".so", ".dylib", ".bin", ".zip", ".tar", ".gz", ".7z", ".rar", ".jar", ".class", ".o", ".a", ".obj", ".pyc"},
		MaxFiles: 5000,
	}
}

// ScanWorkdir 扫描 workdir 下所有文件并生成指纹。
// 第二个返回值为 true 表示因 MaxFiles 截断，结果可能不完整。
func ScanWorkdir(ctx context.Context, workdir string, opts FingerprintOptions) (WorkdirFingerprint, bool, error) {
	result := make(WorkdirFingerprint)
	if strings.TrimSpace(workdir) == "" {
		return result, false, nil
	}
	skipDirSet := setOf(opts.SkipDirs)
	skipExtSet := setOf(lowerSlice(opts.SkipExts))
	truncated := false

	walkErr := filepath.Walk(workdir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if info.IsDir() {
			if path == workdir {
				return nil
			}
			if _, skip := skipDirSet[info.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if _, skip := skipExtSet[ext]; skip {
			return nil
		}
		if opts.MaxFiles > 0 && len(result) >= opts.MaxFiles {
			truncated = true
			return errSkipScan
		}
		rel, relErr := filepath.Rel(workdir, path)
		if relErr != nil {
			return relErr
		}
		hash, hashErr := hashHead(path, fingerprintHeadBytes)
		if hashErr != nil {
			return nil
		}
		result[filepath.ToSlash(rel)] = FileFingerprint{
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			HeadHash: hash,
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errSkipScan) {
		return result, truncated, walkErr
	}
	return result, truncated, nil
}

// DiffFingerprints 对比两个指纹快照，返回新增/删除/修改的相对路径列表（按字典序）。
func DiffFingerprints(before, after WorkdirFingerprint) FingerprintDiff {
	diff := FingerprintDiff{}
	for path, fp := range after {
		prev, ok := before[path]
		if !ok {
			diff.Added = append(diff.Added, path)
			continue
		}
		if !fingerprintEqual(prev, fp) {
			diff.Modified = append(diff.Modified, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			diff.Deleted = append(diff.Deleted, path)
		}
	}
	sort.Strings(diff.Added)
	sort.Strings(diff.Modified)
	sort.Strings(diff.Deleted)
	return diff
}

func fingerprintEqual(a, b FileFingerprint) bool {
	if a.Size != b.Size {
		return false
	}
	if !a.ModTime.Equal(b.ModTime) {
		return false
	}
	return a.HeadHash == b.HeadHash
}

func hashHead(path string, max int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	_, err = io.CopyN(h, f, int64(max))
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:fingerprintHashLen], nil
}

func setOf(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}

func lowerSlice(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.ToLower(strings.TrimSpace(v))
	}
	return out
}

// errSkipScan 用于在 walk 过程中提前终止后续遍历，避免使用 panic/recover。
var errSkipScan = errors.New("scan-truncated")
