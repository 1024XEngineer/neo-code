package infra

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
)

// CollectWorkspaceFiles 在工作区内收集可用于引用补全的文件路径。
func CollectWorkspaceFiles(root string, limit int) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var (
		candidates []string
		limitErr   = errors.New("file limit reached")
	)

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".gocache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		candidates = append(candidates, filepath.ToSlash(rel))
		if limit > 0 && len(candidates) >= limit {
			return limitErr
		}
		return nil
	})
	if err != nil && !errors.Is(err, limitErr) {
		return nil, err
	}

	sort.Strings(candidates)
	return candidates, nil
}
