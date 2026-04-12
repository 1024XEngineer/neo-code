package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/memo"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	tuiservices "neo-code/internal/tui/services"
	tuistate "neo-code/internal/tui/state"
)

type stubProviderService struct {
	providers []configstate.ProviderOption
	models    []providertypes.ModelDescriptor
}

func (s stubProviderService) ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error) {
	return s.providers, nil
}

func (s stubProviderService) SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error) {
	modelID := ""
	if len(s.models) > 0 {
		modelID = s.models[0].ID
	}
	return configstate.Selection{ProviderID: providerID, ModelID: modelID}, nil
}

func (s stubProviderService) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return s.models, nil
}

func (s stubProviderService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return s.models, nil
}

func (s stubProviderService) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
	providerID := ""
	if len(s.providers) > 0 {
		providerID = s.providers[0].ID
	}
	return configstate.Selection{ProviderID: providerID, ModelID: modelID}, nil
}

type stubRuntime struct {
	events        chan agentruntime.RuntimeEvent
	resolveCalls  []agentruntime.PermissionResolutionInput
	resolveErr    error
	cancelInvoked bool
}

func newStubRuntime() *stubRuntime {
	return &stubRuntime{events: make(chan agentruntime.RuntimeEvent)}
}

func (s *stubRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	return nil
}

func (s *stubRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (s *stubRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	s.resolveCalls = append(s.resolveCalls, input)
	return s.resolveErr
}

func (s *stubRuntime) CancelActiveRun() bool {
	s.cancelInvoked = true
	return true
}

func (s *stubRuntime) Events() <-chan agentruntime.RuntimeEvent {
	return s.events
}

func (s *stubRuntime) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return nil, nil
}

func (s *stubRuntime) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return agentsession.NewWithWorkdir("draft", ""), nil
}

func (s *stubRuntime) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error) {
	return agentsession.NewWithWorkdir("draft", workdir), nil
}

func newDefaultAppConfig() *config.Config {
	cfg := config.StaticDefaults()
	cfg.Providers = config.DefaultProviders()
	if len(cfg.Providers) > 0 {
		cfg.SelectedProvider = cfg.Providers[0].Name
		cfg.CurrentModel = cfg.Providers[0].Model
	}
	return cfg
}

func newTestApp(t *testing.T) (App, *stubRuntime) {
	t.Helper()

	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()

	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var providers []configstate.ProviderOption
	var models []providertypes.ModelDescriptor
	if len(cfg.Providers) > 0 {
		provider := cfg.Providers[0]
		providers = []configstate.ProviderOption{
			{
				ID:   provider.Name,
				Name: provider.Name,
				Models: []providertypes.ModelDescriptor{
					{ID: provider.Model, Name: provider.Model},
				},
			},
		}
		models = []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}
	}

	runtime := newStubRuntime()
	app, err := newApp(tuibootstrap.Container{
		Config:          *cfg,
		ConfigManager:   manager,
		Runtime:         runtime,
		ProviderService: stubProviderService{providers: providers, models: models},
	})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}

	return app, runtime
}

func TestAppUpdateBasic(t *testing.T) {
	app, _ := newTestApp(t)

	windowMsg := tea.WindowSizeMsg{Width: 100, Height: 30}
	model, cmd := app.Update(windowMsg)
	if model == nil {
		t.Error("Update returned nil model for WindowSizeMsg")
	}
	app = model.(App)
	if cmd != nil {
		t.Error("Update returned non-nil cmd for WindowSizeMsg")
	}

	app.state.StatusText = ""
	closedMsg := RuntimeClosedMsg{}
	model, cmd = app.Update(closedMsg)
	if model == nil {
		t.Error("Update returned nil model for RuntimeClosedMsg")
	}
	app = model.(App)
	if cmd != nil {
		t.Error("Update returned non-nil cmd for RuntimeClosedMsg")
	}
	if app.state.StatusText != statusRuntimeClosed {
		t.Errorf("Expected status %s, got %s", statusRuntimeClosed, app.state.StatusText)
	}

	runErrMsg := runFinishedMsg{Err: errors.New("test error")}
	model, cmd = app.Update(runErrMsg)
	if model == nil {
		t.Error("Update returned nil model for runFinishedMsg with error")
	}
	app = model.(App)
	if cmd != nil {
		t.Error("Update returned non-nil cmd for runFinishedMsg with error")
	}

	canceledMsg := runFinishedMsg{Err: context.Canceled}
	model, cmd = app.Update(canceledMsg)
	if model == nil {
		t.Error("Update returned nil model for runFinishedMsg with canceled error")
	}
	app = model.(App)
	if cmd != nil {
		t.Error("Update returned non-nil cmd for runFinishedMsg with canceled error")
	}
}

func TestParsePermissionShortcutFromKeyInput(t *testing.T) {
	if decision, ok := parsePermissionShortcut("y"); !ok || decision != agentruntime.PermissionResolutionAllowOnce {
		t.Fatalf("expected allow_once, got %v (ok=%v)", decision, ok)
	}
	if decision, ok := parsePermissionShortcut("a"); !ok || decision != agentruntime.PermissionResolutionAllowSession {
		t.Fatalf("expected allow_session, got %v (ok=%v)", decision, ok)
	}
	if decision, ok := parsePermissionShortcut("n"); !ok || decision != agentruntime.PermissionResolutionReject {
		t.Fatalf("expected reject, got %v (ok=%v)", decision, ok)
	}
	if _, ok := parsePermissionShortcut("x"); ok {
		t.Fatalf("expected unsupported key to return false")
	}
}

func TestRuntimeEventPermissionRequestHandler(t *testing.T) {
	app, _ := newTestApp(t)

	payload := agentruntime.PermissionRequestPayload{
		RequestID: "perm-1",
		ToolName:  "bash",
		Operation: "write",
		Target:    "file.txt",
	}
	handled := runtimeEventPermissionRequestHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected handler to return false")
	}
	if app.pendingPermission == nil || app.pendingPermission.Request.RequestID != "perm-1" {
		t.Fatalf("expected pending permission request to be set")
	}
	if app.state.StatusText != statusPermissionRequired {
		t.Fatalf("expected permission required status, got %s", app.state.StatusText)
	}
}

func TestRuntimeEventPermissionResolvedHandler(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-2"},
	}

	payload := agentruntime.PermissionResolvedPayload{
		RequestID:  "perm-2",
		ToolName:   "bash",
		Decision:   "allow",
		ResolvedAs: "approved",
	}
	handled := runtimeEventPermissionResolvedHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected handler to return false")
	}
	if app.pendingPermission != nil {
		t.Fatalf("expected pending permission to be cleared")
	}
	if app.state.StatusText != "Permission approved" {
		t.Fatalf("expected resolved status text, got %s", app.state.StatusText)
	}
}

func TestUpdatePermissionResolveFlow(t *testing.T) {
	app, runtime := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-3"},
	}

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if model == nil {
		t.Fatalf("expected non-nil model")
	}
	app = model.(App)
	if cmd == nil {
		t.Fatalf("expected command to resolve permission")
	}
	if app.state.StatusText != statusPermissionSubmitting {
		t.Fatalf("expected submitting status, got %s", app.state.StatusText)
	}

	msg := cmd()
	if len(runtime.resolveCalls) != 1 || runtime.resolveCalls[0].RequestID != "perm-3" {
		t.Fatalf("expected ResolvePermission to be called")
	}
	if runtime.resolveCalls[0].Decision != agentruntime.PermissionResolutionAllowOnce {
		t.Fatalf("unexpected decision forwarded: %s", runtime.resolveCalls[0].Decision)
	}

	next, _ := app.Update(msg)
	app = next.(App)
	if app.pendingPermission != nil {
		t.Fatalf("expected pending permission to be cleared after submit")
	}
	if app.state.StatusText != statusPermissionSubmitted {
		t.Fatalf("expected submitted status, got %s", app.state.StatusText)
	}
}

func TestUpdatePermissionResolvedError(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request:    agentruntime.PermissionRequestPayload{RequestID: "perm-4"},
		Submitting: true,
	}

	model, _ := app.Update(permissionResolutionFinishedMsg{
		RequestID: "perm-4",
		Decision:  agentruntime.PermissionResolutionAllowOnce,
		Err:       errors.New("boom"),
	})
	app = model.(App)

	if app.pendingPermission == nil || app.pendingPermission.Submitting {
		t.Fatalf("expected pending permission to remain but leave submitting state")
	}
	if app.state.StatusText != "boom" {
		t.Fatalf("expected failure status, got %s", app.state.StatusText)
	}
}

func TestRunResolvePermissionCommand(t *testing.T) {
	runtime := newStubRuntime()
	cmd := runResolvePermission(runtime, "perm-5", agentruntime.PermissionResolutionAllowSession)
	if cmd == nil {
		t.Fatalf("expected command")
	}
	msg := cmd()
	resolved, ok := msg.(permissionResolutionFinishedMsg)
	if !ok {
		t.Fatalf("expected permissionResolutionFinishedMsg, got %T", msg)
	}
	if resolved.RequestID != "perm-5" || resolved.Decision != agentruntime.PermissionResolutionAllowSession {
		t.Fatalf("unexpected resolved msg: %#v", resolved)
	}
	if len(runtime.resolveCalls) != 1 {
		t.Fatalf("expected resolve call recorded")
	}
}

func TestRenderPermissionPromptInUpdateFlow(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{
			RequestID: "perm-6",
			ToolName:  "bash",
			Operation: "write",
			Target:    "file.txt",
		},
	}
	got := app.renderPermissionPrompt()
	if !strings.Contains(got, "Permission request: bash (write)") {
		t.Fatalf("expected permission prompt header, got %q", got)
	}
}

func TestUpdatePermissionResolutionFinishedMsgIgnoresMismatch(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-7"},
	}
	model, cmd := app.Update(permissionResolutionFinishedMsg{
		RequestID: "perm-8",
		Decision:  agentruntime.PermissionResolutionAllowOnce,
	})
	if model == nil {
		t.Fatalf("expected model")
	}
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected nil cmd")
	}
	if app.pendingPermission == nil || app.pendingPermission.Request.RequestID != "perm-7" {
		t.Fatalf("expected pending permission to remain")
	}
}

func TestRuntimeEventPermissionRequestUsesToolName(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.PermissionRequestPayload{
		RequestID: "perm-9",
		ToolName:  "webfetch",
	}
	runtimeEventPermissionRequestHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if app.pendingPermission == nil || app.pendingPermission.Request.ToolName != "webfetch" {
		t.Fatalf("expected pending permission tool to be set")
	}
}

func TestUpdatePermissionRejectFlow(t *testing.T) {
	app, runtime := newTestApp(t)
	app.pendingPermission = &permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{RequestID: "perm-10"},
	}
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd == nil {
		t.Fatalf("expected resolve cmd")
	}
	app = model.(App)
	msg := cmd()
	next, _ := app.Update(msg)
	app = next.(App)
	if len(runtime.resolveCalls) != 1 || runtime.resolveCalls[0].Decision != agentruntime.PermissionResolutionReject {
		t.Fatalf("expected reject decision to be submitted")
	}
	if app.state.StatusText != statusPermissionSubmitted {
		t.Fatalf("expected submitted status, got %s", app.state.StatusText)
	}
}

func TestRuntimeEventToolResultHandlerUpdatesMessages(t *testing.T) {
	app, _ := newTestApp(t)
	result := tools.ToolResult{
		Name:       "bash",
		Content:    "ok",
		IsError:    false,
		ToolCallID: "tool-1",
	}
	handled := runtimeEventToolResultHandler(&app, agentruntime.RuntimeEvent{Payload: result})
	if !handled {
		t.Fatalf("expected handler to return true")
	}
	last := app.activeMessages[len(app.activeMessages)-1]
	if last.Role != roleTool || last.Content != "ok" {
		t.Fatalf("unexpected tool message: %#v", last)
	}
}

func TestRuntimeEventToolResultHandlerError(t *testing.T) {
	app, _ := newTestApp(t)
	result := tools.ToolResult{
		Name:       "bash",
		Content:    "boom",
		IsError:    true,
		ToolCallID: "tool-2",
	}
	handled := runtimeEventToolResultHandler(&app, agentruntime.RuntimeEvent{Payload: result})
	if !handled {
		t.Fatalf("expected handler to return true")
	}
	if app.state.StatusText != statusToolError {
		t.Fatalf("expected tool error status, got %s", app.state.StatusText)
	}
}

func TestRuntimeEventAgentDoneHandlerAppendsMessage(t *testing.T) {
	app, _ := newTestApp(t)
	payload := providertypes.Message{Role: roleAssistant, Content: "done"}
	handled := runtimeEventAgentDoneHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if !handled {
		t.Fatalf("expected handler to return true")
	}
	if len(app.activeMessages) == 0 {
		t.Fatalf("expected message appended")
	}
}

func TestParseFenceOpenLine(t *testing.T) {
	info, ok := parseFenceOpenLine("```go")
	if !ok || info != "go" {
		t.Fatalf("expected fence info, got %q ok=%v", info, ok)
	}
	info, ok = parseFenceOpenLine(" not a fence")
	if ok || info != "" {
		t.Fatalf("expected no fence")
	}
}

func TestIsFenceCloseLine(t *testing.T) {
	if !isFenceCloseLine("```") {
		t.Fatalf("expected fence close")
	}
	if isFenceCloseLine("```go") {
		t.Fatalf("expected not fence close")
	}
}

func TestIsIndentedCodeLine(t *testing.T) {
	if !isIndentedCodeLine("\tcode") {
		t.Fatalf("expected tab-indented code")
	}
	if !isIndentedCodeLine("    code") {
		t.Fatalf("expected space-indented code")
	}
	if isIndentedCodeLine("code") {
		t.Fatalf("expected non-indented line")
	}
}

func TestTrimCodeIndent(t *testing.T) {
	if got := trimCodeIndent("\tcode"); got != "code" {
		t.Fatalf("expected trimmed tab indent, got %q", got)
	}
	if got := trimCodeIndent("    code"); got != "code" {
		t.Fatalf("expected trimmed space indent, got %q", got)
	}
	if got := trimCodeIndent("code"); got != "code" {
		t.Fatalf("expected unchanged line, got %q", got)
	}
}

func TestSplitMarkdownSegmentsFenced(t *testing.T) {
	content := "hello\n```go\nfmt.Println(\"ok\")\n```\nworld"
	segments := splitMarkdownSegments(content)
	if len(segments) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segments))
	}
	if segments[1].Kind != markdownSegmentCode || segments[1].Code == "" {
		t.Fatalf("expected code segment")
	}
}

func TestSplitMarkdownSegmentsIndented(t *testing.T) {
	content := "hello\n    code line\nworld"
	segments := splitMarkdownSegments(content)
	if len(segments) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segments))
	}
	foundCode := false
	for _, seg := range segments {
		if seg.Kind == markdownSegmentCode && seg.Code != "" {
			foundCode = true
		}
	}
	if !foundCode {
		t.Fatalf("expected indented code segment")
	}
}

func TestSplitIndentedCodeSegmentsDetectsCodeFeaturesInCodeMode(t *testing.T) {
	content := "func main() {\nreturn 1\n}\nplain text"
	segments := splitIndentedCodeSegments(content)
	if len(segments) < 2 {
		t.Fatalf("expected code and text segments, got %d", len(segments))
	}
	if segments[0].Kind != markdownSegmentCode {
		t.Fatalf("expected first segment to be code")
	}
	if !strings.Contains(segments[0].Code, "return 1") {
		t.Fatalf("expected code segment to include return statement, got %q", segments[0].Code)
	}
}

func TestExtractFencedCodeBlocks(t *testing.T) {
	content := "text\n```go\nfmt.Println(\"ok\")\n```\nend"
	blocks := extractFencedCodeBlocks(content)
	if len(blocks) != 1 || blocks[0] == "" {
		t.Fatalf("expected one code block")
	}
}

func TestParseCopyCodeButton(t *testing.T) {
	id, start, end, ok := parseCopyCodeButton("[Copy code #12]")
	if !ok || id != 12 || start >= end {
		t.Fatalf("unexpected parse result: id=%d start=%d end=%d ok=%v", id, start, end, ok)
	}
	if _, _, _, ok := parseCopyCodeButton("no button"); ok {
		t.Fatalf("expected no button parse")
	}
}

func TestCopyCodeBlockByIDSuccess(t *testing.T) {
	app, _ := newTestApp(t)

	var got string
	originalClipboard := clipboardWriteAll
	clipboardWriteAll = func(text string) error {
		got = text
		return nil
	}
	defer func() { clipboardWriteAll = originalClipboard }()

	app.setCodeCopyBlocks([]copyCodeButtonBinding{{ID: 1, Code: "code"}})
	ok := app.copyCodeBlockByID(1)
	if !ok {
		t.Fatalf("expected handled copy")
	}
	if got != "code" {
		t.Fatalf("expected clipboard content, got %q", got)
	}
	if app.state.StatusText == "" {
		t.Fatalf("expected status text to be set")
	}
}

func TestCopyCodeBlockByIDMissing(t *testing.T) {
	app, _ := newTestApp(t)

	ok := app.copyCodeBlockByID(99)
	if !ok {
		t.Fatalf("expected handled copy")
	}
	if app.state.StatusText != statusCodeCopyError {
		t.Fatalf("expected error status, got %s", app.state.StatusText)
	}
}

func TestCopyCodeBlockByIDClipboardError(t *testing.T) {
	app, _ := newTestApp(t)

	originalClipboard := clipboardWriteAll
	clipboardWriteAll = func(text string) error {
		return errors.New("fail")
	}
	defer func() { clipboardWriteAll = originalClipboard }()

	app.setCodeCopyBlocks([]copyCodeButtonBinding{{ID: 2, Code: "code"}})
	ok := app.copyCodeBlockByID(2)
	if !ok {
		t.Fatalf("expected handled copy")
	}
	if app.state.StatusText != statusCodeCopyError {
		t.Fatalf("expected error status, got %s", app.state.StatusText)
	}
}

func TestIsWorkspaceCommandInput(t *testing.T) {
	if !isWorkspaceCommandInput("& ls -la") {
		t.Fatalf("expected workspace command prefix to be detected")
	}
	if isWorkspaceCommandInput("ls -la") {
		t.Fatalf("expected non-workspace command to be false")
	}
}

func TestExtractWorkspaceCommand(t *testing.T) {
	command, err := extractWorkspaceCommand("& git status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if command != "git status" {
		t.Fatalf("expected command to be extracted, got %q", command)
	}

	if _, err := extractWorkspaceCommand("&"); err == nil {
		t.Fatalf("expected error for empty command")
	}
	if _, err := extractWorkspaceCommand("git status"); err == nil {
		t.Fatalf("expected error for missing prefix")
	}
}

func TestFormatWorkspaceCommandResult(t *testing.T) {
	output := "clean\n"
	got := formatWorkspaceCommandResult("git status", output, nil)
	if !strings.Contains(got, "Command: & git status") {
		t.Fatalf("expected success header, got %q", got)
	}
	if !strings.Contains(got, "clean") {
		t.Fatalf("expected output to be included")
	}

	errResult := formatWorkspaceCommandResult("git status", "", errors.New("boom"))
	if !strings.Contains(errResult, "Command Failed: & git status") {
		t.Fatalf("expected failure header, got %q", errResult)
	}
	if !strings.Contains(errResult, "boom") {
		t.Fatalf("expected error message in result")
	}
}

func TestTokenRangeFirstToken(t *testing.T) {
	start, end, token, ok := tokenRange("  /help now", tokenSelectorFirst)
	if !ok {
		t.Fatalf("expected token range to be found")
	}
	if token != "/help" {
		t.Fatalf("expected first token to be /help, got %q", token)
	}
	if start < 0 || end <= start {
		t.Fatalf("expected valid range, got %d-%d", start, end)
	}
}

func TestTokenRangeLastToken(t *testing.T) {
	start, end, token, ok := tokenRange("one two three", tokenSelectorLast)
	if !ok {
		t.Fatalf("expected token range to be found")
	}
	if token != "three" {
		t.Fatalf("expected last token to be three, got %q", token)
	}
	if start < 0 || end <= start {
		t.Fatalf("expected valid range, got %d-%d", start, end)
	}
}

func TestCollectFileSuggestionMatches(t *testing.T) {
	candidates := []string{"README.md", "docs/guide.md", "internal/app.go"}
	matches := collectFileSuggestionMatches("read", candidates, 2)
	if len(matches) == 0 {
		t.Fatalf("expected matches for read")
	}
}

func TestShellArgsAndPowerShellUTF8(t *testing.T) {
	args := shellArgs("bash", "echo hi")
	if len(args) == 0 {
		t.Fatalf("expected shell args to be returned")
	}
	utf8 := powershellUTF8Command("echo hi")
	if utf8 == "" {
		t.Fatalf("expected powershell utf8 command")
	}
}

func TestSanitizeAndDecodeWorkspaceOutput(t *testing.T) {
	raw := []byte("hello\u0000world")
	sanitized := sanitizeWorkspaceOutput(raw)
	if sanitized == "" {
		t.Fatalf("expected sanitized output")
	}
	decoded := decodeWorkspaceOutput(raw)
	if decoded == "" {
		t.Fatalf("expected decoded output")
	}
}

func TestViewSmallWindow(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 60
	app.height = 20

	view := app.View()
	if !strings.Contains(view, "Window too small") {
		t.Fatalf("expected small window warning, got %q", view)
	}
}

func TestComputeLayoutStackedAndWide(t *testing.T) {
	app, _ := newTestApp(t)

	app.width = 90
	app.height = 40
	layout := app.computeLayout()
	if !layout.stacked {
		t.Fatalf("expected stacked layout for narrow width")
	}
	if layout.rightWidth <= 0 || layout.sidebarWidth <= 0 {
		t.Fatalf("expected positive layout widths, got %+v", layout)
	}

	app.width = 140
	app.height = 40
	layout = app.computeLayout()
	if layout.stacked {
		t.Fatalf("expected non-stacked layout for wide width")
	}
	if layout.rightWidth <= 0 || layout.sidebarWidth <= 0 {
		t.Fatalf("expected positive layout widths, got %+v", layout)
	}
}

func TestStatusBadgeVariants(t *testing.T) {
	app, _ := newTestApp(t)

	errorBadge := app.statusBadge("Error occurred")
	if strings.TrimSpace(errorBadge) == "" {
		t.Fatalf("expected error badge to render")
	}

	cancelBadge := app.statusBadge("Canceled")
	if strings.TrimSpace(cancelBadge) == "" {
		t.Fatalf("expected cancel badge to render")
	}

	app.state.IsAgentRunning = true
	runningBadge := app.statusBadge("Running")
	if strings.TrimSpace(runningBadge) == "" {
		t.Fatalf("expected running badge to render")
	}

	app.state.IsAgentRunning = false
	okBadge := app.statusBadge("Ready")
	if strings.TrimSpace(okBadge) == "" {
		t.Fatalf("expected success badge to render")
	}
}

func TestHelpHeightAndRenderHelp(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120

	app.state.ShowHelp = false
	helpHeight := app.helpHeight(80)
	if helpHeight <= 0 {
		t.Fatalf("expected help height to be positive")
	}
	rendered := app.renderHelp(80)
	if strings.TrimSpace(rendered) == "" {
		t.Fatalf("expected renderHelp output")
	}

	app.state.ShowHelp = true
	helpHeight = app.helpHeight(80)
	if helpHeight <= 0 {
		t.Fatalf("expected help height to be positive when help is shown")
	}
}

func TestNewWithBootstrapSuccess(t *testing.T) {
	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	if len(cfg.Providers) > 0 {
		cfg.SelectedProvider = cfg.Providers[0].Name
		cfg.CurrentModel = cfg.Providers[0].Model
	}

	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var providers []configstate.ProviderOption
	var models []providertypes.ModelDescriptor
	if len(cfg.Providers) > 0 {
		provider := cfg.Providers[0]
		providers = []configstate.ProviderOption{
			{
				ID:   provider.Name,
				Name: provider.Name,
				Models: []providertypes.ModelDescriptor{
					{ID: provider.Model, Name: provider.Model},
				},
			},
		}
		models = []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}
	}

	runtime := newStubRuntime()
	app, err := NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   manager,
		Runtime:         runtime,
		ProviderService: stubProviderService{providers: providers, models: models},
	})
	if err != nil {
		t.Fatalf("NewWithBootstrap() error = %v", err)
	}

	cmd := app.Init()
	if cmd == nil {
		t.Fatalf("expected Init() to return command")
	}
}

func TestNewWithBootstrapMissingDependencies(t *testing.T) {
	cfg := newDefaultAppConfig()

	manager := config.NewManager(config.NewLoader(t.TempDir(), cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if _, err := NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   manager,
		Runtime:         nil,
		ProviderService: stubProviderService{},
	}); err == nil {
		t.Fatalf("expected error for nil runtime")
	}

	if _, err := NewWithBootstrap(tuibootstrap.Options{
		Config:          cfg,
		ConfigManager:   nil,
		Runtime:         newStubRuntime(),
		ProviderService: stubProviderService{},
	}); err == nil {
		t.Fatalf("expected error for nil config manager")
	}
}

func TestNewUsesBootstrap(t *testing.T) {
	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	if len(cfg.Providers) > 0 {
		cfg.SelectedProvider = cfg.Providers[0].Name
		cfg.CurrentModel = cfg.Providers[0].Model
	}

	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var providers []configstate.ProviderOption
	var models []providertypes.ModelDescriptor
	if len(cfg.Providers) > 0 {
		provider := cfg.Providers[0]
		providers = []configstate.ProviderOption{
			{
				ID:   provider.Name,
				Name: provider.Name,
				Models: []providertypes.ModelDescriptor{
					{ID: provider.Model, Name: provider.Model},
				},
			},
		}
		models = []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}
	}

	app, err := New(cfg, manager, newStubRuntime(), stubProviderService{providers: providers, models: models})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.state.CurrentProvider == "" {
		t.Fatalf("expected CurrentProvider to be set")
	}
}

func TestRuntimeEventUserMessageHandler(t *testing.T) {
	app, _ := newTestApp(t)
	event := agentruntime.RuntimeEvent{RunID: "run-1"}
	handled := runtimeEventUserMessageHandler(&app, event)
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.ActiveRunID != "run-1" {
		t.Fatalf("expected run id to be set")
	}
	if app.state.StatusText != statusThinking {
		t.Fatalf("expected thinking status")
	}
}

func TestRuntimeEventRunContextHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := tuiservices.RuntimeRunContextPayload{
		Provider: "p1",
		Model:    "m1",
		Workdir:  "/tmp",
	}
	event := agentruntime.RuntimeEvent{RunID: "run-2", SessionID: "s1", Payload: payload}
	handled := runtimeEventRunContextHandler(&app, event)
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.CurrentProvider != "p1" || app.state.CurrentModel != "m1" {
		t.Fatalf("expected provider/model to update")
	}
}

func TestRuntimeEventToolStatusHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := tuiservices.RuntimeToolStatusPayload{ToolCallID: "tool-1", ToolName: "bash", Status: string(tuistate.ToolLifecyclePlanned)}
	handled := runtimeEventToolStatusHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.CurrentTool != "bash" {
		t.Fatalf("expected current tool to be set")
	}
	payload.Status = string(tuistate.ToolLifecycleSucceeded)
	_ = runtimeEventToolStatusHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if app.state.CurrentTool != "" {
		t.Fatalf("expected current tool to be cleared")
	}
}

func TestRuntimeEventUsageHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := tuiservices.RuntimeUsagePayload{Run: tuiservices.RuntimeUsageSnapshot{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}}
	handled := runtimeEventUsageHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.TokenUsage.RunTotalTokens != 3 {
		t.Fatalf("expected token usage to update")
	}
}

func TestRuntimeEventToolCallThinkingHandler(t *testing.T) {
	app, _ := newTestApp(t)
	handled := runtimeEventToolCallThinkingHandler(&app, agentruntime.RuntimeEvent{Payload: "bash"})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.CurrentTool != "bash" {
		t.Fatalf("expected current tool to be set")
	}
}

func TestRuntimeEventToolStartHandler(t *testing.T) {
	app, _ := newTestApp(t)
	call := providertypes.ToolCall{Name: "bash"}
	handled := runtimeEventToolStartHandler(&app, agentruntime.RuntimeEvent{Payload: call})
	if handled {
		t.Fatalf("expected false")
	}
	if app.state.StatusText != statusRunningTool {
		t.Fatalf("expected running tool status")
	}
}

func TestRuntimeEventToolChunkHandler(t *testing.T) {
	app, _ := newTestApp(t)
	_ = runtimeEventToolChunkHandler(&app, agentruntime.RuntimeEvent{Payload: "chunk"})
	if app.state.StatusText != statusRunningTool {
		t.Fatalf("expected running tool status")
	}
}

func TestRuntimeEventAgentChunkHandler(t *testing.T) {
	app, _ := newTestApp(t)
	handled := runtimeEventAgentChunkHandler(&app, agentruntime.RuntimeEvent{Payload: "hello"})
	if !handled {
		t.Fatalf("expected true")
	}
	if len(app.activeMessages) == 0 {
		t.Fatalf("expected message appended")
	}
}

func TestRuntimeEventRunCanceledHandler(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveRunID = "run-3"
	runtimeEventRunCanceledHandler(&app, agentruntime.RuntimeEvent{})
	if app.state.StatusText != statusCanceled {
		t.Fatalf("expected canceled status")
	}
	if app.state.ActiveRunID != "" {
		t.Fatalf("expected run id cleared")
	}
}

func TestRuntimeEventErrorHandler(t *testing.T) {
	app, _ := newTestApp(t)
	runtimeEventErrorHandler(&app, agentruntime.RuntimeEvent{Payload: "boom"})
	if app.state.StatusText != "boom" {
		t.Fatalf("expected status to be set to error")
	}
}

func TestRuntimeEventProviderRetryHandler(t *testing.T) {
	app, _ := newTestApp(t)
	runtimeEventProviderRetryHandler(&app, agentruntime.RuntimeEvent{Payload: "retry"})
	if app.state.StatusText != statusThinking {
		t.Fatalf("expected thinking status")
	}
}

func TestRuntimeEventCompactDoneHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.CompactDonePayload{TriggerMode: "auto", SavedRatio: 0.5, BeforeChars: 10, AfterChars: 5, TranscriptPath: "path"}
	handled := runtimeEventCompactDoneHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if !handled {
		t.Fatalf("expected true")
	}
	if !strings.Contains(app.state.StatusText, "Compact(") {
		t.Fatalf("expected compact status")
	}
}

func TestRuntimeEventCompactErrorHandler(t *testing.T) {
	app, _ := newTestApp(t)
	payload := agentruntime.CompactErrorPayload{TriggerMode: "auto", Message: "fail"}
	handled := runtimeEventCompactErrorHandler(&app, agentruntime.RuntimeEvent{Payload: payload})
	if !handled {
		t.Fatalf("expected true")
	}
	if app.state.ExecutionError == "" {
		t.Fatalf("expected error message")
	}
}

func TestAppendAssistantAndInlineMessage(t *testing.T) {
	app, _ := newTestApp(t)
	app.appendAssistantChunk("hi")
	app.appendAssistantChunk(" there")
	if len(app.activeMessages) == 0 || !strings.Contains(app.activeMessages[len(app.activeMessages)-1].Content, "there") {
		t.Fatalf("expected assistant chunk to append")
	}
	app.appendInlineMessage(roleSystem, "  note ")
	if len(app.activeMessages) < 2 {
		t.Fatalf("expected inline message appended")
	}
}

func TestShouldHandleTabAsInput(t *testing.T) {
	app, _ := newTestApp(t)
	app.focus = panelInput
	app.state.ActivePicker = pickerNone
	app.input.SetValue("/he")
	if !app.shouldHandleTabAsInput(tea.KeyMsg{Type: tea.KeyTab}) {
		t.Fatalf("expected tab to be handled as input")
	}
	app.input.SetValue("")
	if app.shouldHandleTabAsInput(tea.KeyMsg{Type: tea.KeyTab}) {
		t.Fatalf("expected tab to be ignored for empty input")
	}
}

func TestFocusNextPrev(t *testing.T) {
	app, _ := newTestApp(t)
	app.focus = panelSessions
	app.focusNext()
	if app.focus == panelSessions {
		t.Fatalf("expected focus to move")
	}
	app.focusPrev()
}

func TestHandleViewportKeys(t *testing.T) {
	app, _ := newTestApp(t)
	app.transcript.SetContent("line1\nline2\nline3")
	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyDown})
	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyUp})
}

func TestUpdateEnterHelpOpensHelpPicker(t *testing.T) {
	app, _ := newTestApp(t)
	app.input.SetValue("/help")
	app.state.InputText = "/help"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil {
		t.Fatalf("expected non-nil model")
	}
	app = model.(App)
	if cmd != nil {
		t.Fatalf("expected no async cmd when opening help picker")
	}
	if app.state.ActivePicker != pickerHelp {
		t.Fatalf("expected help picker to be active")
	}
	if app.state.StatusText != statusChooseHelp {
		t.Fatalf("expected status %q, got %q", statusChooseHelp, app.state.StatusText)
	}
	if len(app.helpPicker.Items()) != len(builtinSlashCommands) {
		t.Fatalf("expected %d help options, got %d", len(builtinSlashCommands), len(app.helpPicker.Items()))
	}
}

func TestUpdatePickerHelpSelectionOpensModelPicker(t *testing.T) {
	app, _ := newTestApp(t)
	app.refreshHelpPicker()
	app.openHelpPicker()
	selectPickerItemByID(&app.helpPicker, slashCommandModelPick)

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil {
		t.Fatalf("expected model")
	}
	app = model.(App)
	if cmd != nil {
		_ = cmd()
	}
	if app.state.ActivePicker != pickerModel {
		t.Fatalf("expected model picker to open from help selection")
	}
}

func TestUpdatePickerHelpSelectionRunsSlashCommand(t *testing.T) {
	app, _ := newTestApp(t)
	app.refreshHelpPicker()
	app.openHelpPicker()
	selectPickerItemByID(&app.helpPicker, slashCommandStatus)

	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyEnter})
	if model == nil {
		t.Fatalf("expected model")
	}
	app = model.(App)
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected help picker to close after selecting /status")
	}
	if cmd == nil {
		t.Fatalf("expected local slash command cmd")
	}
	msg := cmd()
	result, ok := msg.(localCommandResultMsg)
	if !ok {
		t.Fatalf("expected localCommandResultMsg, got %T", msg)
	}
	if !strings.Contains(result.Notice, "Status:") {
		t.Fatalf("expected status output in slash result, got %q", result.Notice)
	}
}

func TestRunSlashCommandSelectionModelReturnsRefreshCmd(t *testing.T) {
	app, _ := newTestApp(t)
	app.modelRefreshID = ""

	cmd := app.runSlashCommandSelection(slashCommandModelPick)
	if app.state.ActivePicker != pickerModel {
		t.Fatalf("expected model picker to open")
	}
	if cmd == nil {
		t.Fatalf("expected model refresh cmd")
	}
	msg := cmd()
	if _, ok := msg.(modelCatalogRefreshMsg); !ok {
		t.Fatalf("expected modelCatalogRefreshMsg, got %T", msg)
	}
}

func TestRunSlashCommandSelectionProviderRefreshError(t *testing.T) {
	app, _ := newTestApp(t)
	app.providerSvc = errorProviderService{err: errors.New("provider refresh failed")}

	cmd := app.runSlashCommandSelection(slashCommandProvider)
	if cmd != nil {
		t.Fatalf("expected nil cmd when provider refresh fails")
	}
	if !strings.Contains(app.state.StatusText, "provider refresh failed") {
		t.Fatalf("expected provider refresh error status, got %q", app.state.StatusText)
	}
}

func TestRunSlashCommandSelectionModelRefreshError(t *testing.T) {
	app, _ := newTestApp(t)
	app.providerSvc = errorProviderService{err: errors.New("model refresh failed")}

	cmd := app.runSlashCommandSelection(slashCommandModelPick)
	if cmd != nil {
		t.Fatalf("expected nil cmd when model refresh fails")
	}
	if !strings.Contains(app.state.StatusText, "model refresh failed") {
		t.Fatalf("expected model refresh error status, got %q", app.state.StatusText)
	}
}

func TestRunSlashCommandSelectionWorkspaceAndLocal(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = ""
	app.state.CurrentWorkdir = t.TempDir()

	// /cwd 不是 handleImmediateSlashCommand 处理的命令，也不是 switch 中的已知命令，
	// 所以走 default 分支返回 runLocalCommand -> localCommandResultMsg
	localCmd := app.runSlashCommandSelection("/cwd")
	if localCmd == nil {
		t.Fatalf("expected local slash cmd for /cwd")
	}

	statusCmd := app.runSlashCommandSelection(slashCommandStatus)
	if statusCmd == nil {
		t.Fatalf("expected local slash cmd for status")
	}
	statusMsg := statusCmd()
	statusResult, ok := statusMsg.(localCommandResultMsg)
	if !ok {
		t.Fatalf("expected localCommandResultMsg, got %T", statusMsg)
	}
	if !strings.Contains(statusResult.Notice, "Status:") {
		t.Fatalf("expected status output in local command result")
	}
}

func TestHandleImmediateSlashCommandCompactBranches(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-1"

	handled, cmd := app.handleImmediateSlashCommand(slashCommandCompact + " now")
	if !handled || cmd != nil {
		t.Fatalf("expected compact with args to be handled without cmd")
	}
	if !strings.Contains(app.state.StatusText, "usage:") {
		t.Fatalf("expected usage error for compact with args")
	}

	app.state.ExecutionError = ""
	app.state.IsCompacting = true
	handled, cmd = app.handleImmediateSlashCommand(slashCommandCompact)
	if !handled || cmd != nil {
		t.Fatalf("expected compact busy branch to return handled with nil cmd")
	}
	if !strings.Contains(app.state.StatusText, "already running") {
		t.Fatalf("expected busy message")
	}

	app.state.IsCompacting = false
	app.state.IsAgentRunning = false
	app.state.StatusText = ""
	handled, cmd = app.handleImmediateSlashCommand(slashCommandCompact)
	if !handled || cmd == nil {
		t.Fatalf("expected compact success branch to return cmd")
	}
	msg := cmd()
	if _, ok := msg.(compactFinishedMsg); !ok {
		t.Fatalf("expected compactFinishedMsg, got %T", msg)
	}
	if len(runtime.resolveCalls) != 0 {
		t.Fatalf("compact should not resolve permissions")
	}
}

func TestHandleImmediateSlashCommandDefault(t *testing.T) {
	app, _ := newTestApp(t)
	handled, cmd := app.handleImmediateSlashCommand("/unknown")
	if handled || cmd != nil {
		t.Fatalf("expected unknown slash command to be ignored")
	}
}

func TestFormatPermissionPromptToolOnly(t *testing.T) {
	lines := formatPermissionPromptLines(permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{ToolName: "bash"},
	})
	if len(lines) == 0 || !strings.Contains(lines[0], "Permission request: bash") {
		t.Fatalf("expected tool-only prompt header, got %#v", lines)
	}
}

func TestStartDraftSessionResetsRunState(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-1"
	app.state.ActiveSessionTitle = "Session 1"
	app.state.ActiveRunID = "run-1"
	app.state.CurrentTool = "bash"
	app.state.ToolStates = []tuistate.ToolState{{ToolCallID: "tool-1", ToolName: "bash"}}
	app.state.RunContext = tuistate.ContextWindowState{Provider: "openai"}
	app.state.TokenUsage = tuistate.TokenUsageState{RunTotalTokens: 123}
	app.activities = []tuistate.ActivityEntry{{Title: "activity"}}
	app.state.CurrentWorkdir = t.TempDir()

	app.startDraftSession()

	if app.state.ActiveRunID != "" {
		t.Fatalf("expected run id to be reset")
	}
	if app.state.CurrentTool != "" {
		t.Fatalf("expected current tool to be reset")
	}
	if len(app.state.ToolStates) != 0 {
		t.Fatalf("expected tool states to be reset")
	}
	if app.state.ActiveSessionID != "" || app.state.ActiveSessionTitle != draftSessionTitle {
		t.Fatalf("expected draft session state")
	}
	if len(app.activities) != 0 {
		t.Fatalf("expected activities to be cleared")
	}
}
func TestSetCurrentWorkdir(t *testing.T) {
	app, _ := newTestApp(t)

	t.Run("accepts absolute path", func(t *testing.T) {
		dir := t.TempDir()
		app.setCurrentWorkdir(dir)
		if app.state.CurrentWorkdir != filepath.Clean(dir) {
			t.Fatalf("expected %q, got %q", filepath.Clean(dir), app.state.CurrentWorkdir)
		}
	})

	t.Run("ignores empty", func(t *testing.T) {
		app.state.CurrentWorkdir = "/original"
		app.setCurrentWorkdir("")
		if app.state.CurrentWorkdir != "/original" {
			t.Fatalf("expected no change, got %q", app.state.CurrentWorkdir)
		}
	})

	t.Run("ignores whitespace", func(t *testing.T) {
		app.state.CurrentWorkdir = "/original"
		app.setCurrentWorkdir("   ")
		if app.state.CurrentWorkdir != "/original" {
			t.Fatalf("expected no change, got %q", app.state.CurrentWorkdir)
		}
	})

	t.Run("ignores relative path", func(t *testing.T) {
		app.state.CurrentWorkdir = "/original"
		app.setCurrentWorkdir("relative/path")
		if app.state.CurrentWorkdir != "/original" {
			t.Fatalf("expected no change, got %q", app.state.CurrentWorkdir)
		}
	})
}

// newTestAppWithMemo 创建一个注入了 memo 服务的测试 App。
func newTestAppWithMemo(t *testing.T) (App, *stubRuntime) {
	t.Helper()

	cfg := newDefaultAppConfig()
	cfg.Workdir = t.TempDir()
	cfg.Memo.Enabled = true
	if len(cfg.Providers) > 0 {
		cfg.SelectedProvider = cfg.Providers[0].Name
		cfg.CurrentModel = cfg.Providers[0].Model
	}

	manager := config.NewManager(config.NewLoader(cfg.Workdir, cfg))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var providers []configstate.ProviderOption
	var models []providertypes.ModelDescriptor
	if len(cfg.Providers) > 0 {
		provider := cfg.Providers[0]
		providers = []configstate.ProviderOption{
			{ID: provider.Name, Name: provider.Name, Models: []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}},
		}
		models = []providertypes.ModelDescriptor{{ID: provider.Model, Name: provider.Model}}
	}

	// 创建真实的 memo 服务
	memoStore := memo.NewFileStore(t.TempDir(), cfg.Workdir)
	memoSvc := memo.NewService(memoStore, nil, cfg.Memo, nil)

	runtime := newStubRuntime()
	app, err := newApp(tuibootstrap.Container{
		Config:          *cfg,
		ConfigManager:   manager,
		Runtime:         runtime,
		ProviderService: stubProviderService{providers: providers, models: models},
		MemoSvc:         memoSvc,
	})
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}
	return app, runtime
}

func TestHandleMemoCommand(t *testing.T) {
	t.Parallel()

	t.Run("shows no memos message when empty", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		cmd := app.handleMemoCommand()
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		msgs := app.activeMessages
		if len(msgs) == 0 {
			t.Fatal("expected at least one inline message")
		}
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "No memos stored yet") {
			t.Errorf("expected 'no memos' message, got: %s", last.Content)
		}
	})

	t.Run("lists entries when memos exist", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.memoSvc.Add(context.Background(), memo.Entry{Type: memo.TypeUser, Title: "test entry", Content: "test", Source: memo.SourceUserManual})

		app.handleMemoCommand()
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "1 memo(s)") {
			t.Errorf("expected memo count, got: %s", last.Content)
		}
		if !strings.Contains(last.Content, "test entry") {
			t.Errorf("expected entry title, got: %s", last.Content)
		}
	})

	t.Run("nil memoSvc shows error", func(t *testing.T) {
		app, _ := newTestApp(t)
		cmd := app.handleMemoCommand()
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		msgs := app.activeMessages
		if len(msgs) == 0 {
			t.Fatal("expected at least one inline message")
		}
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "not enabled") {
			t.Errorf("expected 'not enabled' message, got: %s", last.Content)
		}
	})
}

func TestHandleRememberCommand(t *testing.T) {
	t.Parallel()

	t.Run("saves memo and shows confirmation", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		cmd := app.handleRememberCommand("my preference")
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "Memo saved") {
			t.Errorf("expected saved confirmation, got: %s", last.Content)
		}
		// Verify the entry was actually saved
		entries, _ := app.memoSvc.List(context.Background())
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		if entries[0].Title != "my preference" {
			t.Errorf("Title = %q, want %q", entries[0].Title, "my preference")
		}
	})

	t.Run("empty text shows usage", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.handleRememberCommand("")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "Usage") {
			t.Errorf("expected usage message, got: %s", last.Content)
		}
	})

	t.Run("whitespace only text shows usage", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.handleRememberCommand("   ")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "Usage") {
			t.Errorf("expected usage message, got: %s", last.Content)
		}
	})

	t.Run("nil memoSvc shows error", func(t *testing.T) {
		app, _ := newTestApp(t)
		app.handleRememberCommand("something")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "not enabled") {
			t.Errorf("expected 'not enabled' message, got: %s", last.Content)
		}
	})
}

func TestHandleForgetCommand(t *testing.T) {
	t.Parallel()

	t.Run("removes matching memos", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.memoSvc.Add(context.Background(), memo.Entry{Type: memo.TypeUser, Title: "remove me", Content: "test", Source: memo.SourceUserManual})
		app.memoSvc.Add(context.Background(), memo.Entry{Type: memo.TypeFeedback, Title: "keep this", Content: "test2", Source: memo.SourceUserManual})

		app.handleForgetCommand("remove")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "Removed 1 memo") {
			t.Errorf("expected removal confirmation, got: %s", last.Content)
		}
		// Verify only one was removed
		entries, _ := app.memoSvc.List(context.Background())
		if len(entries) != 1 {
			t.Fatalf("expected 1 remaining entry, got %d", len(entries))
		}
		if entries[0].Title != "keep this" {
			t.Errorf("remaining entry Title = %q, want %q", entries[0].Title, "keep this")
		}
	})

	t.Run("no match shows message", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.handleForgetCommand("nonexistent")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "No memos matching") {
			t.Errorf("expected no match message, got: %s", last.Content)
		}
	})

	t.Run("empty keyword shows usage", func(t *testing.T) {
		app, _ := newTestAppWithMemo(t)
		app.handleForgetCommand("")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "Usage") {
			t.Errorf("expected usage message, got: %s", last.Content)
		}
	})

	t.Run("nil memoSvc shows error", func(t *testing.T) {
		app, _ := newTestApp(t)
		app.handleForgetCommand("something")
		msgs := app.activeMessages
		last := msgs[len(msgs)-1]
		if !strings.Contains(last.Content, "not enabled") {
			t.Errorf("expected 'not enabled' message, got: %s", last.Content)
		}
	})
}
