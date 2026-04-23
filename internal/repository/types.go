package repository

import "context"

// ChangedFileStatus 表示仓库变更条目的归一化状态。
type ChangedFileStatus string

const (
	StatusAdded      ChangedFileStatus = "added"
	StatusModified   ChangedFileStatus = "modified"
	StatusDeleted    ChangedFileStatus = "deleted"
	StatusRenamed    ChangedFileStatus = "renamed"
	StatusUntracked  ChangedFileStatus = "untracked"
	StatusConflicted ChangedFileStatus = "conflicted"
)

// RetrievalMode 表示定向检索的模式。
type RetrievalMode string

const (
	RetrievalModePath   RetrievalMode = "path"
	RetrievalModeGlob   RetrievalMode = "glob"
	RetrievalModeText   RetrievalMode = "text"
	RetrievalModeSymbol RetrievalMode = "symbol"
)

// Summary 描述当前工作区相对仓库的最小事实快照。
type Summary struct {
	InGitRepo                  bool
	Branch                     string
	Dirty                      bool
	Ahead                      int
	Behind                     int
	ChangedFileCount           int
	RepresentativeChangedFiles []string
}

// ChangedFilesOptions 控制变更上下文的输出上限与片段策略。
type ChangedFilesOptions struct {
	Limit                 int
	IncludeSnippets       bool
	SnippetFileCountLimit int
}

// ChangedFilesContext 表示围绕当前变更集裁剪后的结构化上下文。
type ChangedFilesContext struct {
	Files         []ChangedFile
	Truncated     bool
	ReturnedCount int
	TotalCount    int
}

// ChangedFile 表示单个变更文件的结构化条目。
type ChangedFile struct {
	Path    string
	OldPath string
	Status  ChangedFileStatus
	Snippet string
}

// RetrievalQuery 定义统一的定向检索请求。
type RetrievalQuery struct {
	Mode         RetrievalMode
	Value        string
	ScopeDir     string
	Limit        int
	ContextLines int
}

// RetrievalHit 表示单个检索命中的结构化结果。
type RetrievalHit struct {
	Path          string
	Kind          string
	SymbolOrQuery string
	Snippet       string
	LineHint      int
}

// Service 提供轻量仓库摘要、变更上下文与定向检索能力。
type Service struct {
	gitRunner gitCommandRunner
	readFile  fileReader
}

type snippetResult struct {
	text      string
	lines     int
	truncated bool
}

// NewService 返回默认的轻量仓库服务实现。
func NewService() *Service {
	return &Service{
		gitRunner: runGitCommand,
		readFile:  readFile,
	}
}

// Summary 返回 workdir 的结构化仓库摘要。
func (s *Service) Summary(ctx context.Context, workdir string) (Summary, error) {
	snapshot, err := s.loadGitSnapshot(ctx, workdir)
	if err != nil {
		return Summary{}, err
	}
	if !snapshot.InGitRepo {
		return Summary{}, nil
	}

	paths := make([]string, 0, minInt(len(snapshot.Entries), representativeChangedFilesLimit))
	for index, entry := range snapshot.Entries {
		if index >= representativeChangedFilesLimit {
			break
		}
		paths = append(paths, entry.Path)
	}

	return Summary{
		InGitRepo:                  true,
		Branch:                     snapshot.Branch,
		Dirty:                      len(snapshot.Entries) > 0,
		Ahead:                      snapshot.Ahead,
		Behind:                     snapshot.Behind,
		ChangedFileCount:           len(snapshot.Entries),
		RepresentativeChangedFiles: paths,
	}, nil
}

// ChangedFiles 返回围绕当前变更集裁剪后的结构化上下文。
func (s *Service) ChangedFiles(ctx context.Context, workdir string, opts ChangedFilesOptions) (ChangedFilesContext, error) {
	snapshot, err := s.loadGitSnapshot(ctx, workdir)
	if err != nil {
		return ChangedFilesContext{}, err
	}
	if !snapshot.InGitRepo {
		return ChangedFilesContext{}, nil
	}

	limit := normalizeLimit(opts.Limit, defaultChangedFilesLimit, maxChangedFilesLimit)
	includeSnippets := opts.IncludeSnippets
	if includeSnippets && opts.SnippetFileCountLimit > 0 && len(snapshot.Entries) > opts.SnippetFileCountLimit {
		includeSnippets = false
	}
	entries := snapshot.Entries
	truncated := false
	if len(entries) > limit {
		entries = entries[:limit]
		truncated = true
	}

	files := make([]ChangedFile, 0, len(entries))
	totalSnippetLines := 0
	for _, entry := range entries {
		file := ChangedFile{
			Path:    entry.Path,
			OldPath: entry.OldPath,
			Status:  entry.Status,
		}
		if includeSnippets {
			snippet, snippetErr := s.changedFileSnippet(ctx, workdir, entry)
			if snippetErr != nil {
				return ChangedFilesContext{}, snippetErr
			}
			if snippet.truncated {
				truncated = true
			}
			if snippet.text != "" {
				remaining := maxChangedSnippetTotalLines - totalSnippetLines
				if remaining <= 0 {
					truncated = true
				} else {
					finalSnippet := trimSnippetText(snippet.text, remaining)
					if finalSnippet.truncated || snippet.lines > remaining {
						truncated = true
					}
					file.Snippet = finalSnippet.text
					totalSnippetLines += finalSnippet.lines
				}
			}
		}
		files = append(files, file)
	}

	return ChangedFilesContext{
		Files:         files,
		Truncated:     truncated,
		ReturnedCount: len(files),
		TotalCount:    len(snapshot.Entries),
	}, nil
}

// Retrieve 根据模式返回受限且结构化的定向检索结果。
func (s *Service) Retrieve(ctx context.Context, workdir string, query RetrievalQuery) ([]RetrievalHit, error) {
	root, scope, normalized, err := normalizeRetrievalQuery(workdir, query)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	switch normalized.Mode {
	case RetrievalModePath:
		return s.retrieveByPath(ctx, root, normalized)
	case RetrievalModeGlob:
		return s.retrieveByGlob(ctx, root, scope, normalized)
	case RetrievalModeText:
		return s.retrieveByText(ctx, root, scope, normalized, false)
	case RetrievalModeSymbol:
		return s.retrieveBySymbol(ctx, root, scope, normalized)
	default:
		return nil, errInvalidMode
	}
}
