package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/skills"
	tuiservices "neo-code/internal/tui/services"
)

func TestFormatAvailableSkills(t *testing.T) {
	t.Parallel()

	if got := formatAvailableSkills(nil, ""); !strings.Contains(got, "No skills found") {
		t.Fatalf("expected empty message, got %q", got)
	}

	text := formatAvailableSkills([]agentruntime.AvailableSkillState{
		{
			Descriptor: skills.Descriptor{
				ID:          "go-review",
				Description: "review go code",
				Scope:       skills.ScopeSession,
				Version:     "v1",
				Source:      skills.Source{Kind: skills.SourceKindLocal},
			},
			Active: true,
		},
	}, "session-1")
	if !strings.Contains(text, "go-review [active]") {
		t.Fatalf("expected active entry, got %q", text)
	}
}

func TestFormatSessionSkills(t *testing.T) {
	t.Parallel()

	if got := formatSessionSkills(nil); !strings.Contains(got, "No active skills") {
		t.Fatalf("expected empty active message, got %q", got)
	}

	text := formatSessionSkills([]agentruntime.SessionSkillState{
		{SkillID: "missing", Missing: true},
		{SkillID: "go-review", Descriptor: &skills.Descriptor{ID: "go-review", Description: "review"}},
	})
	if !strings.Contains(text, "missing [missing]") {
		t.Fatalf("expected missing entry, got %q", text)
	}
	if !strings.Contains(text, "go-review [active]") {
		t.Fatalf("expected active entry, got %q", text)
	}
}

func TestSkillCommandErrorAndPlaceholderHelpers(t *testing.T) {
	t.Parallel()

	if !isSkillUsagePlaceholder("<id>") {
		t.Fatalf("expected placeholder marker")
	}
	if isSkillUsagePlaceholder("go-review") {
		t.Fatalf("did not expect normal id as placeholder")
	}

	unsupported := normalizeSkillCommandError(tuiservices.ErrUnsupportedActionInGatewayMode)
	if unsupported == nil || !strings.Contains(strings.ToLower(unsupported.Error()), "gateway") {
		t.Fatalf("expected gateway hint, got %v", unsupported)
	}
	containsButNotSentinel := errors.New("skill id unsupported_action_in_gateway_mode is invalid")
	if normalizeSkillCommandError(containsButNotSentinel) != containsButNotSentinel {
		t.Fatalf("expected plain error passthrough when only message contains gateway marker")
	}
	plain := errors.New("plain")
	if normalizeSkillCommandError(plain) != plain {
		t.Fatalf("expected non-gateway error passthrough")
	}
	if normalizeSkillCommandError(nil) != nil {
		t.Fatalf("expected nil error passthrough")
	}
}

func TestHandleSkillCommandUsageBranches(t *testing.T) {
	t.Parallel()

	app, _ := newTestApp(t)

	if cmd := app.handleSkillCommand("active unexpected"); cmd != nil {
		t.Fatalf("expected nil cmd for invalid active usage")
	}
	if !strings.Contains(app.state.StatusText, slashUsageSkillActive) {
		t.Fatalf("expected /skill active usage text, got %q", app.state.StatusText)
	}

	if cmd := app.handleSkillCommand("unknown go-review"); cmd != nil {
		t.Fatalf("expected nil cmd for unknown action")
	}
	if !strings.Contains(app.state.StatusText, "usage: /skill use <id> | /skill off <id> | /skill active") {
		t.Fatalf("expected generic skill usage text, got %q", app.state.StatusText)
	}
}

func TestHandleSkillUseAndOffValidationBranches(t *testing.T) {
	t.Parallel()

	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-skills"

	if cmd := app.handleSkillUseCommand("<id>"); cmd != nil {
		t.Fatalf("expected nil cmd for placeholder id")
	}
	if !strings.Contains(app.state.StatusText, slashUsageSkillUse) {
		t.Fatalf("expected /skill use usage text, got %q", app.state.StatusText)
	}

	if cmd := app.handleSkillOffCommand(" "); cmd != nil {
		t.Fatalf("expected nil cmd for blank id")
	}
	if !strings.Contains(app.state.StatusText, slashUsageSkillOff) {
		t.Fatalf("expected /skill off usage text, got %q", app.state.StatusText)
	}

	app.state.ActiveSessionID = ""
	if cmd := app.handleSkillOffCommand("go-review"); cmd != nil {
		t.Fatalf("expected nil cmd when /skill off has no active session")
	}
	if !strings.Contains(app.state.StatusText, "requires an active session") {
		t.Fatalf("expected active session requirement hint, got %q", app.state.StatusText)
	}
}

func TestHandleSkillsAndActiveCommandErrorBranches(t *testing.T) {
	t.Parallel()

	app, runtime := newTestApp(t)
	runtime.availableSkillsErr = tuiservices.ErrUnsupportedActionInGatewayMode
	runtime.sessionSkillsErr = errors.New("list failed")

	skillsCmd := app.handleSkillsCommand()
	if skillsCmd == nil {
		t.Fatalf("expected /skills cmd")
	}
	model, _ := app.Update(skillsCmd())
	app = model.(App)
	if !strings.Contains(strings.ToLower(app.state.StatusText), "gateway") {
		t.Fatalf("expected gateway hint for /skills error, got %q", app.state.StatusText)
	}

	app.state.ActiveSessionID = ""
	if cmd := app.handleSkillActiveCommand(); cmd != nil {
		t.Fatalf("expected nil cmd when /skill active has no active session")
	}
	if !strings.Contains(app.state.StatusText, "requires an active session") {
		t.Fatalf("expected active session requirement hint, got %q", app.state.StatusText)
	}

	app.state.ActiveSessionID = "session-skills"
	activeCmd := app.handleSkillActiveCommand()
	if activeCmd == nil {
		t.Fatalf("expected /skill active cmd")
	}
	model, _ = app.Update(activeCmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "list failed") {
		t.Fatalf("expected runtime error passthrough for /skill active, got %q", app.state.StatusText)
	}

	runtime.deactivateSkillErr = errors.New("deactivate failed")
	offCmd := app.handleSkillOffCommand("go-review")
	if offCmd == nil {
		t.Fatalf("expected /skill off cmd")
	}
	model, _ = app.Update(offCmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "deactivate failed") {
		t.Fatalf("expected /skill off error passthrough, got %q", app.state.StatusText)
	}
}

func TestFormatHelpersCoverFallbackBranches(t *testing.T) {
	t.Parallel()

	text := formatAvailableSkills([]agentruntime.AvailableSkillState{
		{
			Descriptor: skills.Descriptor{
				ID:          "plain",
				Description: "",
				Scope:       "",
				Version:     " ",
				Source:      skills.Source{Kind: ""},
			},
			Active: false,
		},
	}, "")
	if !strings.Contains(text, "scope=explicit") {
		t.Fatalf("expected explicit scope fallback, got %q", text)
	}
	if !strings.Contains(text, "| -") {
		t.Fatalf("expected empty description fallback, got %q", text)
	}

	sessionText := formatSessionSkills([]agentruntime.SessionSkillState{
		{SkillID: "zeta", Descriptor: nil},
		{SkillID: "Alpha", Descriptor: &skills.Descriptor{ID: "Alpha", Description: ""}},
	})
	if !strings.Contains(sessionText, "- zeta [active]") {
		t.Fatalf("expected descriptor-nil fallback line, got %q", sessionText)
	}
	if !strings.Contains(sessionText, "- Alpha [active] -") {
		t.Fatalf("expected empty-description fallback, got %q", sessionText)
	}
}

func TestFormatSkillHelpersSanitizeAndLimitOutput(t *testing.T) {
	t.Parallel()

	evil := "go\x1b[31m-review"
	longDescription := strings.Repeat("x", maxSkillFieldLength+20)
	text := formatAvailableSkills([]agentruntime.AvailableSkillState{
		{
			Descriptor: skills.Descriptor{
				ID:          evil,
				Description: longDescription,
				Scope:       skills.ScopeSession,
				Version:     "v1",
				Source:      skills.Source{Kind: skills.SourceKindLocal},
			},
			Active: true,
		},
	}, "session-1")
	if strings.Contains(text, "\x1b") {
		t.Fatalf("expected ansi control chars to be stripped, got %q", text)
	}
	if !strings.Contains(text, "go-review [active]") {
		t.Fatalf("expected sanitized skill id, got %q", text)
	}
	if !strings.Contains(text, "...") {
		t.Fatalf("expected long description to be truncated, got %q", text)
	}

	states := make([]agentruntime.AvailableSkillState, 0, maxRenderedSkillsCount+1)
	for i := 0; i < maxRenderedSkillsCount+1; i++ {
		states = append(states, agentruntime.AvailableSkillState{
			Descriptor: skills.Descriptor{
				ID:          fmt.Sprintf("skill-%02d", i),
				Description: "desc",
				Scope:       skills.ScopeSession,
				Version:     "v1",
				Source:      skills.Source{Kind: skills.SourceKindLocal},
			},
		})
	}
	limited := formatAvailableSkills(states, "")
	if !strings.Contains(limited, "... and 1 more skills") {
		t.Fatalf("expected overflow summary, got %q", limited)
	}
}
