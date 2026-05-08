package repository

import "context"

// ChangedFileStatus 表示仓库变更条目的归一化状态。
type ChangedFileStatus string

const (
	StatusAdded      ChangedFileStatus = "added"
	StatusModified   ChangedFileStatus = "modified"
	StatusDeleted    ChangedFileStatus = "deleted"
	StatusRenamed    ChangedFileStatus = "renamed"
	StatusCopied     ChangedFileStatus = "copied"
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

// FileChangeKind 表示两个 checkpoint 之间单个 path 的变更类别。
type FileChangeKind string

const (
	FileChangeAdded    FileChangeKind = "added"
	FileChangeDeleted  FileChangeKind = "deleted"
	FileChangeModified FileChangeKind = "modified"
)

// FileChangeEntry 描述端到端 diff 中单个 path 的变更。
type FileChangeEntry struct {
	Path string
	Kind FileChangeKind
}

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

// InspectOptions 控制一次 inspection 中 changed-files 的裁剪策略。
type InspectOptions struct {
	ChangedFilesLimit                int
	IncludeChangedFileSnippets       bool
	ChangedFileSnippetFileCountLimit int
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

// RetrievalHit 表示单个检索命中的结构化结果（兼容旧上下文，含代码片段）。
type RetrievalHit struct {
	Path          string
	Kind          string
	SymbolOrQuery string
	Snippet       string
	LineHint      int
}

// RetrievalResult 表示一次定向检索的结构化结果与截断状态。
type RetrievalResult struct {
	Hits      []RetrievalHit
	Truncated bool
}

// InspectResult 表示一次共享快照 inspection 产出的仓库摘要与变更上下文。
type InspectResult struct {
	Summary      Summary
	ChangedFiles ChangedFilesContext
}

// ReadOptions 控制 codebase_read 的读取上限。
type ReadOptions struct {
	MaxBytes int
}

// ReadResult 表示一次受限文件读取的结果。
type ReadResult struct {
	Path      string
	Content   string
	Truncated bool
	IsBinary  bool
	Size      int64
}

// SearchOptions 控制文本/符号搜索的裁剪策略。
type SearchOptions struct {
	ScopeDir  string
	Limit     int
	WholeWord bool
}

// TextSearchHit 表示文本搜索的单文件命中（硬约束：不返回代码内容）。
type TextSearchHit struct {
	Path       string
	LineHint   int
	MatchCount int
}

// TextSearchResult 表示文本搜索的结构化结果。
type TextSearchResult struct {
	Hits       []TextSearchHit
	Truncated  bool
	TotalCount int
}

// SymbolSearchHit 表示符号搜索的单条命中（硬约束：仅返回位置与签名，不含函数体）。
type SymbolSearchHit struct {
	Path      string
	LineHint  int
	Kind      string
	Signature string
}

// SymbolSearchResult 表示符号搜索的结构化结果。
type SymbolSearchResult struct {
	Hits       []SymbolSearchHit
	Truncated  bool
	TotalCount int
}

// Service 提供轻量仓库摘要、变更上下文、定向检索与代码库探索能力。
type Service struct {
	gitRunner       GitCommandRunner
	readFile        FileReader
	treesitterIndex *TreeSitterIndexer
}

// NewService 返回默认的轻量仓库服务实现。
func NewService() *Service {
	return &Service{
		gitRunner:       runGitCommand,
		readFile:        readFile,
		treesitterIndex: NewTreeSitterIndexer(),
	}
}

// Inspect 基于一次共享 git 快照返回仓库摘要与变更上下文。
func (s *Service) Inspect(ctx context.Context, workdir string, opts InspectOptions) (InspectResult, error) {
	snapshot, err := s.loadGitSnapshot(ctx, workdir)
	if err != nil {
		return InspectResult{}, err
	}
	if !snapshot.InGitRepo {
		return InspectResult{}, nil
	}
	changedFiles, err := s.inspectChangedFiles(ctx, workdir, snapshot, opts)
	if err != nil {
		return InspectResult{}, err
	}

	return InspectResult{
		Summary:      summaryFromSnapshot(snapshot),
		ChangedFiles: changedFiles,
	}, nil
}

func (s *Service) inspectChangedFiles(
	ctx context.Context,
	workdir string,
	snapshot gitSnapshot,
	opts InspectOptions,
) (ChangedFilesContext, error) {
	return s.changedFilesFromSnapshot(ctx, workdir, snapshot, ChangedFilesOptions{
		Limit:                 opts.ChangedFilesLimit,
		IncludeSnippets:       opts.IncludeChangedFileSnippets,
		SnippetFileCountLimit: opts.ChangedFileSnippetFileCountLimit,
	})
}

// Summary 返回 workdir 的结构化仓库摘要。
func (s *Service) Summary(ctx context.Context, workdir string) (Summary, error) {
	result, err := s.Inspect(ctx, workdir, InspectOptions{})
	if err != nil {
		return Summary{}, err
	}
	return result.Summary, nil
}

func summaryFromSnapshot(snapshot gitSnapshot) Summary {
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
	}
}

// ChangedFiles 返回围绕当前变更集裁剪后的结构化上下文。
func (s *Service) ChangedFiles(ctx context.Context, workdir string, opts ChangedFilesOptions) (ChangedFilesContext, error) {
	result, err := s.Inspect(ctx, workdir, InspectOptions{
		ChangedFilesLimit:                opts.Limit,
		IncludeChangedFileSnippets:       opts.IncludeSnippets,
		ChangedFileSnippetFileCountLimit: opts.SnippetFileCountLimit,
	})
	if err != nil {
		return ChangedFilesContext{}, err
	}
	return result.ChangedFiles, nil
}

// changedFilesFromSnapshot 基于共享快照派生 changed-files 上下文，避免同轮重复 git 扫描。
func (s *Service) changedFilesFromSnapshot(
	ctx context.Context,
	workdir string,
	snapshot gitSnapshot,
	opts ChangedFilesOptions,
) (ChangedFilesContext, error) {
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
				if isContextError(snippetErr) {
					return ChangedFilesContext{}, snippetErr
				}
				files = append(files, file)
				continue
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
func (s *Service) Retrieve(ctx context.Context, workdir string, query RetrievalQuery) (RetrievalResult, error) {
	root, scope, normalized, err := normalizeRetrievalQuery(workdir, query)
	if err != nil {
		return RetrievalResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RetrievalResult{}, err
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
		return RetrievalResult{}, errInvalidMode
	}
}

type snippetResult struct {
	text      string
	lines     int
	truncated bool
}
