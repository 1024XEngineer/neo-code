package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/config"
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
	app.height = 50

	view := app.View()
	plain := copyCodeANSIPattern.ReplaceAllString(view, "")
	if !strings.Contains(plain, "AI-POWERED CLI WORKSPACE") {
		t.Fatalf("expected startup subtitle in view")
	}
	if !strings.Contains(plain, "Quick Actions") {
		t.Fatalf("expected startup action description in view")
	}
}

func TestMinimumDocHeightStartupUsesHeaderMenuPromptAndHint(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.activeMessages = nil
	app.width = 100
	app.input.SetValue("/he")
	app.state.InputText = "/he"
	app.applyComponentLayout(true)
	app.refreshCommandMenu()

	contentWidth := max(0, app.width-app.styles.doc.GetHorizontalFrameSize())
	want := headerBarHeight +
		app.commandMenuHeight(contentWidth, 0) +
		lipgloss.Height(app.renderPrompt(contentWidth)) +
		app.helpHeight(contentWidth)
	if got := app.minimumDocHeight(contentWidth); got != want {
		t.Fatalf("expected startup minimum doc height %d, got %d", want, got)
	}
}

func TestViewStartupUsesDynamicMinimumHeightThreshold(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.activeMessages = nil
	app.width = 100
	app.input.SetValue("/he")
	app.state.InputText = "/he"
	app.applyComponentLayout(true)
	app.refreshCommandMenu()

	contentWidth := max(0, app.width-app.styles.doc.GetHorizontalFrameSize())
	minDocHeight := app.minimumDocHeight(contentWidth)

	app.height = app.styles.doc.GetVerticalFrameSize() + minDocHeight - 1
	view := app.View()
	if !strings.Contains(view, "Window too small") {
		t.Fatalf("expected startup dynamic minimum height warning at %d", minDocHeight-1)
	}
	if !strings.Contains(view, fmt.Sprintf("%dx%d", minWindowWidth, minDocHeight)) {
		t.Fatalf("expected startup dynamic size hint %dx%d, got %q", minWindowWidth, minDocHeight, view)
	}

	app.height++
	view = app.View()
	if strings.Contains(view, "Window too small") {
		t.Fatalf("expected startup view to render at dynamic minimum height %d", minDocHeight)
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

	app.modelScopeGuide = &modelScopeGuideState{
		ProviderID: config.ModelScopeName,
		APIKeyEnv:  config.ModelScopeDefaultAPIKeyEnv,
		Step:       modelScopeGuideStepPasteToken,
		Token:      "test-token",
	}
	app.state.ActivePicker = pickerModelScope
	modelScopeView := app.renderPicker(48, 14)
	if !strings.Contains(modelScopeView, modelScopeGuideTitle) {
		t.Fatalf("expected modelscope guide title")
	}
}

func TestRenderModelScopeGuideBranches(t *testing.T) {
	app, _ := newTestApp(t)
	if got := app.renderModelScopeGuide(); !strings.Contains(got, "not active") {
		t.Fatalf("expected inactive guide message, got %q", got)
	}

	app.modelScopeGuide = &modelScopeGuideState{
		ProviderID: config.ModelScopeName,
		APIKeyEnv:  config.ModelScopeDefaultAPIKeyEnv,
		GuidePath:  "/tmp/modelscope-guide.html",
		Step:       modelScopeGuideStepGuide,
	}
	guideView := app.renderModelScopeGuide()
	if !strings.Contains(guideView, "Step 1/4") || !strings.Contains(guideView, "Guide HTML: /tmp/modelscope-guide.html") {
		t.Fatalf("expected step 1 guide content, got %q", guideView)
	}

	app.modelScopeGuide.Step = modelScopeGuideStepLogin
	loginView := app.renderModelScopeGuide()
	if !strings.Contains(loginView, "Step 2/4") {
		t.Fatalf("expected step 2 login content, got %q", loginView)
	}

	app.modelScopeGuide.Step = modelScopeGuideStepToken
	tokenView := app.renderModelScopeGuide()
	if !strings.Contains(tokenView, "Step 3/4") {
		t.Fatalf("expected step 3 token content, got %q", tokenView)
	}

	app.modelScopeGuide.Step = modelScopeGuideStepPasteToken
	app.modelScopeGuide.Token = "abc123xyz"
	app.modelScopeGuide.Notice = "notice text"
	app.modelScopeGuide.Error = "error text"
	app.modelScopeGuide.Submitting = true
	pasteView := app.renderModelScopeGuide()
	if !strings.Contains(pasteView, "Step 4/4") {
		t.Fatalf("expected step 4 paste token content, got %q", pasteView)
	}
	if strings.Contains(pasteView, app.modelScopeGuide.Token) {
		t.Fatalf("expected token to be masked in view, got %q", pasteView)
	}
	if !strings.Contains(pasteView, "[Notice] notice text") || !strings.Contains(pasteView, "[Error] error text") {
		t.Fatalf("expected notice and error sections, got %q", pasteView)
	}
	if !strings.Contains(pasteView, "Submitting token...") {
		t.Fatalf("expected submitting hint, got %q", pasteView)
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

func TestRenderWaterfallDoesNotShowSelectionCopyHint(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActivePicker = pickerNone
	app.input.SetValue("hello")
	app.textSelection.active = true
	app.textSelection.startLine = 0
	app.textSelection.endLine = 0

	view := app.renderWaterfall(80, 24)
	if strings.Contains(view, "宸查€夋嫨鍐呭") {
		t.Fatalf("expected selection copy hint to be hidden")
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

	view := app.renderWaterfall(100, 50)
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

func TestStartupWidthsClampToAvailableSpace(t *testing.T) {
	widths := []int{24, 32, 48, 64, 96}
	for _, width := range widths {
		maxContent := max(1, width-4)
		if got := startupContentWidth(width); got > maxContent {
			t.Fatalf("expected startup content width <= %d for viewport %d, got %d", maxContent, width, got)
		}

		maxPrompt := max(1, width-2)
		if got := startupPromptWidth(width); got > maxPrompt {
			t.Fatalf("expected startup prompt width <= %d for viewport %d, got %d", maxPrompt, width, got)
		}
	}
}

func TestRenderStartupScreenHidesQuickActionsBelowFullLayoutThreshold(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.activeMessages = nil
	app.input.SetValue("")

	const viewportWidth = 72
	const viewportHeight = 29
	view := copyCodeANSIPattern.ReplaceAllString(app.renderWaterfall(viewportWidth, viewportHeight), "")
	if strings.Contains(view, "Quick Actions") {
		t.Fatalf("expected startup quick actions hidden below %dx%d", startupLayoutFullMinWidth, startupLayoutFullMinHeight)
	}

	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > viewportWidth {
			t.Fatalf("expected line width <= %d, got %d in line %q", viewportWidth, got, line)
		}
	}
}

func TestRenderWaterfallStartupWithCommandSuggestionsFitsViewport(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.activeMessages = nil
	app.input.SetValue("/he")
	app.state.InputText = "/he"
	app.width = 100
	app.height = 32
	app.applyComponentLayout(true)
	app.refreshCommandMenu()

	view := copyCodeANSIPattern.ReplaceAllString(app.renderWaterfall(100, 32), "")
	if !strings.Contains(view, commandMenuTitle) {
		t.Fatalf("expected command suggestion menu to be rendered on startup")
	}
	if !strings.Contains(view, "/he") {
		t.Fatalf("expected prompt input to stay visible with startup command suggestions, got %q", view)
	}
	if got := lipgloss.Height(view); got != 32 {
		t.Fatalf("expected startup waterfall height to match viewport, got %d", got)
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 100 {
			t.Fatalf("expected line width <= %d, got %d in line %q", 100, got, line)
		}
	}
}

func TestRenderPromptUsesAvailableWidthOnStartup(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.activeMessages = nil
	app.input.SetValue("")

	const viewportWidth = 112
	prompt := copyCodeANSIPattern.ReplaceAllString(app.renderPrompt(viewportWidth), "")
	if got := lipgloss.Height(prompt); got > 4 {
		t.Fatalf("expected startup prompt to stay on one content row, got height %d: %q", got, prompt)
	}
	for _, line := range strings.Split(prompt, "\n") {
		if got := lipgloss.Width(line); got > viewportWidth {
			t.Fatalf("expected prompt line width <= %d, got %d in line %q", viewportWidth, got, line)
		}
	}
}

func TestRenderStartupScreenQuickActionsHasNoBorderBox(t *testing.T) {
	app, _ := newTestApp(t)
	app.startupScreenLocked = true
	app.state.ActivePicker = pickerNone
	app.activeMessages = nil
	app.input.SetValue("")

	view := copyCodeANSIPattern.ReplaceAllString(app.renderStartupScreen(100, 20), "")
	if strings.ContainsRune(view, '\u256d') ||
		strings.ContainsRune(view, '\u256e') ||
		strings.ContainsRune(view, '\u2570') ||
		strings.ContainsRune(view, '\u256f') ||
		strings.ContainsRune(view, '\u2502') ||
		strings.ContainsRune(view, '\u2500') {
		t.Fatalf("expected quick actions section without box border, got %q", view)
	}
}

func TestRenderStartupScreenCompactMenuAndInvalidSize(t *testing.T) {
	app, _ := newTestApp(t)

	if got := app.renderStartupScreen(0, 20); got != "" {
		t.Fatalf("expected empty output when width<=0, got %q", got)
	}
	if got := app.renderStartupScreen(80, 0); got != "" {
		t.Fatalf("expected empty output when height<=0, got %q", got)
	}

	compact := copyCodeANSIPattern.ReplaceAllString(app.renderStartupScreen(30, 16), "")
	if !strings.Contains(compact, "NeoCode") {
		t.Fatalf("expected fallback logo text for narrow width, got %q", compact)
	}
}

func TestRenderStartupScreenHidesLogoWhenHeightIsVeryTight(t *testing.T) {
	app, _ := newTestApp(t)

	compact := copyCodeANSIPattern.ReplaceAllString(app.renderStartupScreen(80, 4), "")
	if strings.Contains(compact, "NeoCode") {
		t.Fatalf("expected startup logo hidden when height is very tight, got %q", compact)
	}
	if !strings.Contains(compact, "AI-POWERED CLI WORKSPACE") {
		t.Fatalf("expected startup subtitle to remain visible, got %q", compact)
	}
}

func TestStartupContentWidthForWideViewport(t *testing.T) {
	got := startupContentWidth(200)
	if got != 148 {
		t.Fatalf("expected startupContentWidth(200)=148, got %d", got)
	}
}

func TestStartupPromptWidthForWideViewport(t *testing.T) {
	got := startupPromptWidth(200)
	if got != 156 {
		t.Fatalf("expected startupPromptWidth(200)=156, got %d", got)
	}
}

func TestStartupCenterPadLineCutsLongContent(t *testing.T) {
	got := startupCenterPadLine("0123456789", 4)
	if got != "0123" {
		t.Fatalf("expected cut line to width 4, got %q", got)
	}
}

func TestStartupQuickActionKeyWidthBranches(t *testing.T) {
	if got := startupQuickActionKeyWidth(0); got != 0 {
		t.Fatalf("expected key width 0 for non-positive card width, got %d", got)
	}

	if got := startupQuickActionKeyWidth(10); got != 4 {
		t.Fatalf("expected key width clamp to min 4 for narrow card, got %d", got)
	}

	if got := startupQuickActionKeyWidth(100); got != 6 {
		t.Fatalf("expected key width to keep longest shortcut width 6, got %d", got)
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
	if !strings.Contains(form, "Base URL:   (") {
		t.Fatalf("expected base url hint, got %q", form)
	}
	if !strings.Contains(form, "Chat Endpoint:   (") || !strings.Contains(form, "Chat API Mode") {
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
	if !strings.Contains(view, "Please resize to at least 60x30.") {
		t.Fatalf("expected 60x30 size hint, got %q", view)
	}
}

func TestViewNormalIncludesHeaderAndBody(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 40
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

func TestRenderHelpShowsCtrlNCtrlLAndError(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.StatusText = statusReady
	rendered := copyCodeANSIPattern.ReplaceAllString(app.renderHelp(80), "")
	if !strings.Contains(rendered, "Ctrl+N New chat") {
		t.Fatalf("expected footer help to include new session shortcut, got %q", rendered)
	}
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
	if strings.TrimSpace(assistantPlain) != "" {
		t.Fatalf("expected assistant tool-call-only block to be hidden, got %q", assistantPlain)
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

func TestRenderCommandMenuKeepsFirstRowIndent(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActivePicker = pickerNone
	app.transcript.Width = 80
	app.commandMenu.SetItems([]list.Item{
		commandMenuItem{title: "/help", description: "show help"},
		commandMenuItem{title: "/clear", description: "clear current draft"},
	})
	app.commandMenu.Select(1)
	app.commandMenuMeta = tuistate.CommandMenuMeta{Title: commandMenuTitle}
	app.resizeCommandMenu()

	menu := copyCodeANSIPattern.ReplaceAllString(app.renderCommandMenu(80), "")
	helpLine := ""
	for _, line := range strings.Split(menu, "\n") {
		if strings.Contains(line, "/help") {
			helpLine = line
			break
		}
	}
	if helpLine == "" {
		t.Fatalf("expected /help row in command menu, got %q", menu)
	}
	if strings.Index(helpLine, "/help") < 2 {
		t.Fatalf("expected first row to keep leading indent, got %q", helpLine)
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

	if got := app.transcriptScrollbarWidth(transcriptScrollbarWidth); got != 0 {
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
	} else {
		plain := copyCodeANSIPattern.ReplaceAllString(got, "")
		if strings.ContainsRune(plain, '\u2588') {
			t.Fatalf("expected scrollbar thumb to avoid glyph blocks, got %q", plain)
		}
		if strings.Count(plain, "\n") != 4 {
			t.Fatalf("expected scrollbar height to stay continuous with 5 rows, got %q", plain)
		}
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
