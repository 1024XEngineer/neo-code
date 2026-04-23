package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	tuistate "neo-code/internal/tui/state"
)

type stubMarkdownRenderer struct {
	render func(content string, width int) (string, error)
}

func (s stubMarkdownRenderer) Render(content string, width int) (string, error) {
	return s.render(content, width)
}

func TestRenderPickerHelpMode(t *testing.T) {
	app, _ := newTestApp(t)
	app.refreshHelpPicker()
	app.state.ActivePicker = pickerHelp

	view := app.renderPicker(48, 14)
	if !strings.Contains(view, helpPickerTitle) {
		t.Fatalf("expected help picker title in view")
	}
	if !strings.Contains(view, helpPickerSubtitle) {
		t.Fatalf("expected help picker subtitle in view")
	}
}

func TestViewStartupScreenRendersStartupSections(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.width = 120
	app.height = 36

	view := app.View()
	plain := copyCodeANSIPattern.ReplaceAllString(view, "")
	if !strings.Contains(plain, "AI-POWERED CLI WORKSPACE") {
		t.Fatalf("expected startup subtitle in view")
	}
	if !strings.Contains(plain, "Quick Actions") {
		t.Fatalf("expected startup action description in view")
	}
}

func TestRenderPickerSessionMode(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActivePicker = pickerSession
	app.layoutCached = false
	app.sessionPicker.SetItems([]list.Item{
		sessionItem{Summary: agentsession.Summary{
			ID:        "session-1",
			Title:     "Session One",
			UpdatedAt: time.Now(),
		}},
	})
	app.applyComponentLayout(false)

	view := app.renderPicker(48, 14)
	if !strings.Contains(view, sessionPickerTitle) {
		t.Fatalf("expected session picker title in view")
	}
	if !strings.Contains(view, sessionPickerSubtitle) {
		t.Fatalf("expected session picker subtitle in view")
	}
	if !strings.Contains(view, "Session One") {
		t.Fatalf("expected session item in picker body")
	}
}

func TestRenderPickerProviderAndFileMode(t *testing.T) {
	app, _ := newTestApp(t)

	app.state.ActivePicker = pickerProvider
	app.providerPicker.SetItems([]list.Item{selectionItem{id: "p1", name: "Provider 1"}})
	providerView := app.renderPicker(48, 14)
	if !strings.Contains(providerView, providerPickerTitle) {
		t.Fatalf("expected provider picker title")
	}

	app.state.ActivePicker = pickerFile
	fileView := app.renderPicker(48, 14)
	if !strings.Contains(fileView, filePickerTitle) {
		t.Fatalf("expected file picker title")
	}

	app.startProviderAddForm()
	app.state.ActivePicker = pickerProviderAdd
	providerAddView := app.renderPicker(48, 14)
	if !strings.Contains(providerAddView, providerAddTitle) {
		t.Fatalf("expected provider add title")
	}
}

func TestBuildPickerLayoutExpandsPopupSpace(t *testing.T) {
	app, _ := newTestApp(t)

	got := app.buildPickerLayout(100, 30)
	if got.panelHeight < 20 {
		t.Fatalf("expected expanded picker panel height, got %d", got.panelHeight)
	}
	if got.listHeight < pickerListMinHeight {
		t.Fatalf("expected picker list height >= %d, got %d", pickerListMinHeight, got.listHeight)
	}
	if got.listWidth < pickerListMinWidth {
		t.Fatalf("expected picker list width >= %d, got %d", pickerListMinWidth, got.listWidth)
	}
}

func TestRenderWaterfallUsesDynamicTranscriptHeight(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActivePicker = pickerNone
	app.state.InputText = "test"
	app.input.SetValue("test")
	app.transcript.SetContent("line1\nline2")

	view := app.renderWaterfall(80, 24)
	if strings.TrimSpace(view) == "" {
		t.Fatalf("expected non-empty waterfall view")
	}
}

func TestRenderWaterfallThinkingState(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActivePicker = pickerNone
	app.state.IsAgentRunning = true
	app.state.StatusText = statusThinking

	view := app.renderWaterfall(80, 24)
	if !strings.Contains(view, "Thinking...") {
		t.Fatalf("expected thinking hint in waterfall view")
	}
}

func TestRenderWaterfallShowsStartupScreenForEmptyDraft(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.logViewerVisible = false
	app.state.IsAgentRunning = false
	app.state.IsCompacting = false
	app.activeMessages = nil
	app.input.SetValue("")

	view := app.renderWaterfall(100, 26)
	if !strings.Contains(view, "AI-POWERED CLI WORKSPACE") {
		t.Fatalf("expected startup subtitle in waterfall, got %q", view)
	}
	if !strings.Contains(view, "Ctrl+N") {
		t.Fatalf("expected startup shortcut hint, got %q", view)
	}
}

func TestRenderWaterfallKeepsStartupScreenWhileTypingUntilUnlocked(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.activeMessages = nil
	app.input.SetValue("hello")

	view := app.renderWaterfall(100, 26)
	if !strings.Contains(view, "AI-POWERED CLI WORKSPACE") {
		t.Fatalf("expected startup screen to remain while typing before first send")
	}

	app.startupScreenLocked = false
	view = app.renderWaterfall(100, 26)
	if strings.Contains(view, "AI-POWERED CLI WORKSPACE") {
		t.Fatalf("expected startup screen hidden after unlock")
	}

	app.startupScreenLocked = true
	app.input.SetValue("")
	app.activeMessages = []providertypes.Message{
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("ready")}},
	}
	app.rebuildTranscript()
	view = app.renderWaterfall(100, 26)
	if strings.Contains(view, "AI-POWERED CLI WORKSPACE") {
		t.Fatalf("expected startup screen hidden once transcript has messages")
	}
}

func TestApplyComponentLayoutKeepsTranscriptHeightInSyncWithWaterfall(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.focus = panelInput
	app.activities = []tuistate.ActivityEntry{{Kind: "tool", Title: "running", Detail: "tool call"}}
	app.commandMenu.SetItems([]list.Item{
		commandMenuItem{title: "/help", description: "show help"},
		commandMenuItem{title: "/model", description: "switch model"},
	})
	app.commandMenuMeta = tuistate.CommandMenuMeta{Title: commandMenuTitle}
	app.input.SetValue(strings.Repeat("line\n", 5))
	app.input.SetHeight(app.composerHeight())

	app.applyComponentLayout(false)

	lay := app.computeLayout()
	wantTranscriptHeight, activityHeight, menuHeight, todoHeight := app.waterfallMetrics(lay.contentWidth, lay.contentHeight)
	if app.transcript.Height != wantTranscriptHeight {
		t.Fatalf("expected transcript height %d, got %d", wantTranscriptHeight, app.transcript.Height)
	}

	_, transcriptY, _, transcriptHeight := app.transcriptBounds()
	_, activityY, _, gotActivityHeight := app.activityBounds()
	_, inputY, _, _ := app.inputBounds()
	if transcriptHeight != wantTranscriptHeight {
		t.Fatalf("expected transcript bounds height %d, got %d", wantTranscriptHeight, transcriptHeight)
	}
	if activityY != transcriptY+wantTranscriptHeight {
		t.Fatalf("expected activity Y %d, got %d", transcriptY+wantTranscriptHeight, activityY)
	}
	if gotActivityHeight != activityHeight {
		t.Fatalf("expected activity height %d, got %d", activityHeight, gotActivityHeight)
	}
	if inputY != transcriptY+wantTranscriptHeight+activityHeight+menuHeight {
		t.Fatalf("expected input Y %d, got %d", transcriptY+wantTranscriptHeight+activityHeight+menuHeight, inputY)
	}
	promptHeight := lipgloss.Height(app.renderPrompt(lay.contentWidth))
	if usedHeight := wantTranscriptHeight + activityHeight + todoHeight + menuHeight + promptHeight; usedHeight > lay.contentHeight {
		t.Fatalf("expected waterfall stack to fit content area, used %d > %d", usedHeight, lay.contentHeight)
	}
}

func TestComputeLayoutUsesRenderedHeaderHeight(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 30

	lay := app.computeLayout()
	header := app.renderHeader(lay.contentWidth)
	if got := lipgloss.Height(header); got != headerBarHeight {
		t.Fatalf("expected header height %d, got %d", headerBarHeight, got)
	}
	if strings.Contains(header, "\x1b[") {
		t.Fatalf("expected header to avoid ANSI escapes, got %q", header)
	}
}

func TestRenderUserMessagePlacesTagOnLeft(t *testing.T) {
	app, _ := newTestApp(t)

	block, _ := app.renderMessageBlockWithCopy(providertypes.Message{
		Role:  roleUser,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello left aligned")},
	}, 72, 1)

	plain := copyCodeANSIPattern.ReplaceAllString(block, "")
	lines := strings.Split(plain, "\n")

	var (
		tagLine     string
		contentLine string
	)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(line, messageTagUser) {
			tagLine = line
		}
		if strings.Contains(line, "hello left aligned") {
			contentLine = line
		}
	}
	if tagLine == "" || contentLine == "" {
		t.Fatalf("expected user tag and content lines, got %q", plain)
	}

	tagLeading := len(tagLine) - len(strings.TrimLeft(tagLine, " "))
	if tagLeading > 1 {
		t.Fatalf("expected user tag to be left aligned, got line %q", tagLine)
	}
	contentLeading := len(contentLine) - len(strings.TrimLeft(contentLine, " "))
	if contentLeading < 2 || contentLeading > 6 {
		t.Fatalf("expected user body to keep a small left indent, got %q", contentLine)
	}
}

func TestBuildPickerLayoutClampMin(t *testing.T) {
	app, _ := newTestApp(t)
	got := app.buildPickerLayout(10, 8)
	if got.panelWidth != pickerPanelMinWidth {
		t.Fatalf("expected panel width clamp to min %d, got %d", pickerPanelMinWidth, got.panelWidth)
	}
	if got.panelHeight != pickerPanelMinHeight {
		t.Fatalf("expected panel height clamp to min %d, got %d", pickerPanelMinHeight, got.panelHeight)
	}
}

func TestRenderWaterfallWithActivePicker(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActivePicker = pickerSession
	app.sessionPicker.SetItems([]list.Item{
		sessionItem{Summary: agentsession.Summary{
			ID:        "session-1",
			Title:     "Session One",
			UpdatedAt: time.Now(),
		}},
	})

	view := app.renderWaterfall(90, 24)
	if !strings.Contains(view, sessionPickerTitle) {
		t.Fatalf("expected picker waterfall view to include session picker title")
	}
}

func TestRenderBody(t *testing.T) {
	app, _ := newTestApp(t)
	out := app.renderBody(layout{contentWidth: 90, contentHeight: 24})
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected renderBody output")
	}
}

func TestMaskedSecret(t *testing.T) {
	if got := maskedSecret(""); got != "" {
		t.Fatalf("maskedSecret(empty) = %q, want empty", got)
	}
	if got := maskedSecret("   "); got != "" {
		t.Fatalf("maskedSecret(space) = %q, want empty", got)
	}
	if got := maskedSecret("sk-12345"); got != "******" {
		t.Fatalf("maskedSecret(secret) = %q, want ******", got)
	}
}

func TestRenderProviderAddFormMasksAPIKeyAndShowsHints(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()
	app.providerAddForm.Driver = "openaicompat"
	app.providerAddForm.Name = "team-gateway"
	app.providerAddForm.APIKey = "sk-secret-98765"
	app.providerAddForm.BaseURL = ""
	app.providerAddForm.ChatEndpointPath = ""
	app.providerAddForm.Error = "input invalid"
	app.providerAddForm.ErrorIsHard = true

	form := app.renderProviderAddForm()
	if strings.Contains(form, "sk-secret-98765") {
		t.Fatalf("expected api key to be masked, got %q", form)
	}
	if !strings.Contains(form, "API Key: ******") {
		t.Fatalf("expected masked api key, got %q", form)
	}
	if !strings.Contains(form, "Model Source: discover") {
		t.Fatalf("expected model source field, got %q", form)
	}
	if !strings.Contains(form, "留空会自动填充默认地址") {
		t.Fatalf("expected base url hint, got %q", form)
	}
	if !strings.Contains(form, "Chat Endpoint:   (") || !strings.Contains(form, "Chat API Mode") || !strings.Contains(form, "自动回填默认端点") {
		t.Fatalf("expected chat endpoint auto-fill hint, got %q", form)
	}
	if !strings.Contains(form, "[Error] input invalid") {
		t.Fatalf("expected hard error label, got %q", form)
	}
}

func TestRenderProviderAddFormPromptLabel(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()
	app.providerAddForm.Driver = "anthropic"
	app.providerAddForm.Error = "continue input"
	app.providerAddForm.ErrorIsHard = false

	form := app.renderProviderAddForm()
	if !strings.Contains(form, "[Prompt] continue input") {
		t.Fatalf("expected prompt label, got %q", form)
	}
}

func TestRenderProviderAddFormManualModelsStage(t *testing.T) {
	app, _ := newTestApp(t)
	app.startProviderAddForm()
	app.providerAddForm.Stage = providerAddFormStageManualModels
	app.providerAddForm.ManualModelsJSON = ""

	form := app.renderProviderAddForm()
	if !strings.Contains(form, "Manual Model JSON") {
		t.Fatalf("expected manual model json title, got %q", form)
	}
	if !strings.Contains(form, "\"id\": \"model-id\"") {
		t.Fatalf("expected manual model json template, got %q", form)
	}
}

func TestViewSmallWindowHint(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 40
	app.height = 10

	view := app.View()
	if !strings.Contains(view, "Window too small.") {
		t.Fatalf("expected small-window hint, got %q", view)
	}
}

func TestViewNormalIncludesHeaderAndBody(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 30
	app.state.CurrentModel = "test-model"
	app.state.CurrentWorkdir = "/tmp/workdir"
	app.state.StatusText = "running"
	app.state.IsAgentRunning = true
	app.runProgressKnown = true
	app.runProgressValue = 0.42
	app.runProgressLabel = "loading"
	app.state.InputText = "hi"
	app.input.SetValue("hi")

	view := app.View()
	if strings.TrimSpace(view) == "" {
		t.Fatalf("expected non-empty view")
	}
	if !strings.Contains(view, "NeoCode") {
		t.Fatalf("expected header text, got %q", view)
	}
	if !strings.Contains(view, "42% loading") {
		t.Fatalf("expected progress header, got %q", view)
	}
	if !strings.Contains(view, "cwd: /tmp/workdir") {
		t.Fatalf("expected current workdir in header, got %q", view)
	}
}

func TestViewAddsSpacerWhenDocIsTallerThanContent(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 60

	view := app.View()
	if strings.TrimSpace(view) == "" {
		t.Fatalf("expected non-empty view")
	}
}

func TestRenderHeaderFallbackAndTrim(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.IsAgentRunning = true
	app.state.StatusText = "custom-running-status"
	app.state.CurrentWorkdir = "/tmp/workdir"
	header := app.renderHeader(20)
	if strings.TrimSpace(header) == "" {
		t.Fatalf("expected non-empty header")
	}

	app.state.CurrentModel = strings.Repeat("very-long-model-name-", 4)
	header = app.renderHeader(16)
	if strings.TrimSpace(header) == "" {
		t.Fatalf("expected trimmed header output")
	}
}

func TestRenderHeaderIncludesWorkdirFallback(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.CurrentModel = "test-model"
	app.state.StatusText = statusReady
	app.state.CurrentWorkdir = ""

	header := app.renderHeader(120)
	if !strings.Contains(header, "cwd: -") {
		t.Fatalf("expected workdir fallback in header, got %q", header)
	}
}

func TestComposeHeaderLineKeepsRightSectionVisible(t *testing.T) {
	line := composeHeaderLine("NeoCode / model / Ready", "cwd: /tmp/workdir", 48)
	if !strings.Contains(line, "cwd: /tmp/workdir") {
		t.Fatalf("expected right section in composed header, got %q", line)
	}
	if got := lipgloss.Width(line); got < lipgloss.Width("cwd: /tmp/workdir") {
		t.Fatalf("expected composed header width to include right section, got %d", got)
	}
}

func TestComposeHeaderLineDoesNotOverflowTightWidth(t *testing.T) {
	right := "cwd: /tmp/workdir"
	width := lipgloss.Width(right)
	line := composeHeaderLine("NeoCode / model / status", right, width)
	if got := lipgloss.Width(line); got > width {
		t.Fatalf("expected composed header width <= %d, got %d (%q)", width, got, line)
	}
	if !strings.Contains(line, right) {
		t.Fatalf("expected right section preserved, got %q", line)
	}
}

func TestRenderPanelAndActivityPreview(t *testing.T) {
	app, _ := newTestApp(t)
	panel := app.renderPanel("Title", "Sub", "Body", 60, 8, true)
	if !strings.Contains(panel, "Title") || !strings.Contains(panel, "Body") {
		t.Fatalf("expected panel content, got %q", panel)
	}

	if got := app.renderActivityPreview(60); got != "" {
		t.Fatalf("expected empty activity preview, got %q", got)
	}
	app.activities = []tuistate.ActivityEntry{{Kind: "tool", Title: "Run", Detail: "Detail"}}
	withActivity := app.renderActivityPreview(60)
	if withActivity != "" {
		t.Fatalf("expected activity preview disabled even with entries, got %q", withActivity)
	}

	app.commandMenu.SetItems([]list.Item{
		commandMenuItem{title: "/help", description: "show help"},
	})
	app.commandMenuMeta = tuistate.CommandMenuMeta{Title: commandMenuTitle}
	withMenu := app.renderWaterfall(80, 24)
	if strings.Contains(withMenu, activityTitle) {
		t.Fatalf("expected waterfall to exclude activity panel, got %q", withMenu)
	}
	if !strings.Contains(withMenu, commandMenuTitle) {
		t.Fatalf("expected command menu to be rendered")
	}
}

func TestRenderHelpShowsCtrlLAndError(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.StatusText = statusReady
	rendered := copyCodeANSIPattern.ReplaceAllString(app.renderHelp(80), "")
	if !strings.Contains(rendered, "Ctrl+L Log viewer") {
		t.Fatalf("expected footer help to include log viewer shortcut, got %q", rendered)
	}

	app.showFooterError("permission denied")
	rendered = copyCodeANSIPattern.ReplaceAllString(app.renderHelp(80), "")
	if !strings.Contains(rendered, "Error: permission denied") {
		t.Fatalf("expected footer to surface execution error, got %q", rendered)
	}
}

func TestRenderHelpErrorToastExpires(t *testing.T) {
	app, _ := newTestApp(t)
	base := time.Unix(1_700_000_000, 0)
	app.nowFn = func() time.Time { return base }

	app.showFooterError("permission denied")
	rendered := copyCodeANSIPattern.ReplaceAllString(app.renderHelp(80), "")
	if !strings.Contains(rendered, "Error: permission denied") {
		t.Fatalf("expected footer toast to show immediately, got %q", rendered)
	}

	app.nowFn = func() time.Time { return base.Add(footerErrorFlashDuration + 50*time.Millisecond) }
	rendered = copyCodeANSIPattern.ReplaceAllString(app.renderHelp(80), "")
	if strings.Contains(rendered, "Error: permission denied") {
		t.Fatalf("expected footer toast to auto-hide after flash duration, got %q", rendered)
	}
}

func TestRenderHelpExpandsOnlyWhenErrorVisible(t *testing.T) {
	app, _ := newTestApp(t)
	base := time.Unix(1_700_000_000, 0)
	app.nowFn = func() time.Time { return base }

	noError := app.renderHelp(80)
	noErrorHeight := lipgloss.Height(noError)

	app.showFooterError("permission denied")
	withError := app.renderHelp(80)
	withErrorHeight := lipgloss.Height(withError)
	if withErrorHeight <= noErrorHeight {
		t.Fatalf("expected footer to grow while error is visible, noError=%d withError=%d", noErrorHeight, withErrorHeight)
	}

	app.nowFn = func() time.Time { return base.Add(footerErrorFlashDuration + 50*time.Millisecond) }
	expired := app.renderHelp(80)
	expiredHeight := lipgloss.Height(expired)
	if expiredHeight != noErrorHeight {
		t.Fatalf("expected footer height to return after toast expires, noError=%d expired=%d", noErrorHeight, expiredHeight)
	}
}

func TestRenderMessageContentWithCopyBranches(t *testing.T) {
	app, _ := newTestApp(t)

	app.markdownRenderer = nil
	rendered, bindings := app.renderMessageContentWithCopy("hello", 40, app.styles.messageBody, 1)
	if len(bindings) != 0 || strings.TrimSpace(rendered) == "" {
		t.Fatalf("expected fallback content without bindings, got rendered=%q bindings=%v", rendered, bindings)
	}

	app, _ = newTestApp(t)
	content := "hello\n```go\nfmt.Println(\"x\")\n```\nworld"
	rendered, bindings = app.renderMessageContentWithCopy(content, 60, app.styles.messageBody, 3)
	if strings.TrimSpace(rendered) == "" {
		t.Fatalf("expected rendered markdown content")
	}
	if len(bindings) != 0 {
		t.Fatalf("expected no copy bindings, got %d", len(bindings))
	}

	app, _ = newTestApp(t)
	app.markdownRenderer = stubMarkdownRenderer{
		render: func(content string, width int) (string, error) {
			return "", errors.New("render failed")
		},
	}
	rendered, bindings = app.renderMessageContentWithCopy("plain text", 60, app.styles.messageBody, 1)
	if len(bindings) != 0 || strings.TrimSpace(rendered) == "" {
		t.Fatalf("expected empty message fallback when markdown render fails")
	}
}

func TestRenderMessageContentWithCopyCodeFallbackAndEmptySegments(t *testing.T) {
	app, _ := newTestApp(t)
	app.markdownRenderer = stubMarkdownRenderer{
		render: func(content string, width int) (string, error) {
			if strings.HasPrefix(strings.TrimSpace(content), "```") {
				return "", errors.New("code render failed")
			}
			return "ok", nil
		},
	}
	content := " \n```go\nfmt.Println(\"x\")\n```\n"
	rendered, bindings := app.renderMessageContentWithCopy(content, 60, app.styles.messageBody, 7)
	if strings.TrimSpace(rendered) == "" {
		t.Fatalf("expected rendered output")
	}
	if len(bindings) != 0 {
		t.Fatalf("expected no bindings, got %+v", bindings)
	}
}

func TestRenderMessageBlockWithCopyExtraBranches(t *testing.T) {
	app, _ := newTestApp(t)

	eventBlock, _ := app.renderMessageBlockWithCopy(providertypes.Message{
		Role:  roleEvent,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("event")},
	}, 50, 1)
	if !strings.Contains(eventBlock, "event") {
		t.Fatalf("expected event block")
	}

	toolBlock, bindings := app.renderMessageBlockWithCopy(providertypes.Message{
		Role:  roleTool,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool")},
	}, 50, 1)
	if toolBlock != "" || bindings != nil {
		t.Fatalf("expected tool role to be skipped")
	}

	assistantBlock, _ := app.renderMessageBlockWithCopy(providertypes.Message{
		Role: roleAssistant,
		ToolCalls: []providertypes.ToolCall{
			{Name: "bash"},
		},
	}, 50, 1)
	assistantPlain := copyCodeANSIPattern.ReplaceAllString(assistantBlock, "")
	if !strings.Contains(assistantPlain, "bash") {
		t.Fatalf("expected tool calls summary in assistant block")
	}

	userBlock, _ := app.renderMessageBlockWithCopy(providertypes.Message{
		Role:  roleUser,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
	}, 10, 1)
	if strings.TrimSpace(userBlock) == "" {
		t.Fatalf("expected user message block")
	}
}

func TestRenderProviderAddFormNoFormAndChatEndpointField(t *testing.T) {
	app, _ := newTestApp(t)
	if got := app.renderProviderAddForm(); got != "No form active" {
		t.Fatalf("unexpected no-form output: %q", got)
	}

	app.startProviderAddForm()
	form := app.renderProviderAddForm()
	if !strings.Contains(form, "Chat Endpoint") {
		t.Fatalf("expected chat endpoint field in add form")
	}
}

func TestRenderCommandMenuEmptyBody(t *testing.T) {
	app, _ := newTestApp(t)
	app.commandMenu.SetItems([]list.Item{
		commandMenuItem{title: "/help", description: "show help"},
	})
	app.state.ActivePicker = pickerHelp
	if got := app.renderCommandMenu(50); got != "" {
		t.Fatalf("expected empty menu while picker is active")
	}
}

func TestNormalizeAndTrimHelpers(t *testing.T) {
	trimmed := trimRenderedTrailingWhitespace("line1  \nline2\t")
	if strings.HasSuffix(trimmed, "\t") || strings.HasSuffix(trimmed, " ") {
		t.Fatalf("expected trailing whitespace trimmed, got %q", trimmed)
	}

	normalized := normalizeBlockRightEdge("a\nbb", 6)
	lines := strings.Split(normalized, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two lines, got %q", normalized)
	}
}

func TestRenderLogViewerHonorsOffset(t *testing.T) {
	app, _ := newTestApp(t)
	for i := 0; i < 6; i++ {
		app.logEntries = append(app.logEntries, logEntry{
			Timestamp: time.Unix(int64(i), 0),
			Level:     "info",
			Source:    "test",
			Message:   "msg-" + string(rune('A'+i)),
		})
	}

	app.logViewerOffset = 0
	view := app.renderLogViewer(80, 6)
	if !strings.Contains(view, "msg-F") {
		t.Fatalf("expected newest log message at offset 0, got %q", view)
	}

	app.logViewerOffset = 2
	view = app.renderLogViewer(80, 6)
	if !strings.Contains(view, "msg-D") {
		t.Fatalf("expected older log message at offset 2, got %q", view)
	}
}

func TestRenderActivityLineAndScrollbarHelpers(t *testing.T) {
	app, _ := newTestApp(t)

	line := app.renderActivityLine(tuistate.ActivityEntry{
		Time:    time.Unix(1_700_000_000, 0),
		Kind:    "tool",
		Title:   "Run",
		Detail:  "details",
		IsError: false,
	}, 72)
	if strings.TrimSpace(line) == "" {
		t.Fatalf("expected renderActivityLine to return non-empty text")
	}

	if got := app.transcriptScrollbarWidth(3); got != 0 {
		t.Fatalf("expected narrow transcript width to disable scrollbar, got %d", got)
	}
	if got := app.transcriptScrollbarWidth(20); got != transcriptScrollbarWidth {
		t.Fatalf("expected transcript scrollbar width %d, got %d", transcriptScrollbarWidth, got)
	}

	if got := app.renderTranscriptScrollbar(0, 10); got != "" {
		t.Fatalf("expected empty scrollbar when width is zero, got %q", got)
	}
	if got := app.renderTranscriptScrollbar(2, 0); got != "" {
		t.Fatalf("expected empty scrollbar when height is zero, got %q", got)
	}

	app.transcript.Width = 20
	app.transcript.Height = 5
	app.transcript.SetContent(strings.Repeat("line\n", 30))
	app.transcript.SetYOffset(3)
	if got := app.renderTranscriptScrollbar(2, 5); got == "" {
		t.Fatalf("expected non-empty scrollbar when transcript is scrollable")
	}
}

func TestRenderLogViewerEmptyAndNarrowWidthBranches(t *testing.T) {
	app, _ := newTestApp(t)

	empty := app.renderLogViewer(60, 8)
	if !strings.Contains(empty, "No log entries") {
		t.Fatalf("expected empty log viewer hint, got %q", empty)
	}

	app.logEntries = []logEntry{
		{
			Timestamp: time.Unix(1_700_000_100, 0),
			Level:     "warning",
			Source:    "source-with-long-name",
			Message:   "long message that should be truncated or hidden in narrow layouts",
		},
	}
	narrow := app.renderLogViewer(45, 8)
	if strings.Contains(narrow, "long message") {
		t.Fatalf("expected message text hidden when message width is zero, got %q", narrow)
	}

	wide := app.renderLogViewer(70, 8)
	if !strings.Contains(wide, "Use Up/Down/PgUp/PgDn to scroll") {
		t.Fatalf("expected scroll hint in log viewer footer, got %q", wide)
	}
}

func TestRenderWaterfallAndTranscriptWithoutScrollbar(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActivePicker = pickerNone
	app.logViewerVisible = true
	logView := app.renderWaterfall(80, 20)
	if !strings.Contains(logView, "Log Viewer") {
		t.Fatalf("expected log viewer branch in waterfall, got %q", logView)
	}

	app.logViewerVisible = false
	plain := app.renderTranscriptWithScrollbar(2, "hello")
	if strings.TrimSpace(plain) == "" {
		t.Fatalf("expected transcript to render without scrollbar in very narrow width")
	}
}
