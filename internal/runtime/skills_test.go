package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
)

type stubSkillsRegistry struct {
	skills         map[string]skills.Skill
	getErr         error
	lastListInput  skills.ListInput
	listFilterByWS bool
	refreshErr     error
	refreshFn      func(ctx context.Context) error
	refreshCount   int
}

func (r *stubSkillsRegistry) List(ctx context.Context, input skills.ListInput) ([]skills.Descriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.lastListInput = input
	result := make([]skills.Descriptor, 0, len(r.skills))
	for _, skill := range r.skills {
		if r.listFilterByWS && skill.Descriptor.Scope == skills.ScopeWorkspace && strings.TrimSpace(input.Workspace) == "" {
			continue
		}
		result = append(result, skill.Descriptor)
	}
	return result, nil
}

func (r *stubSkillsRegistry) Get(ctx context.Context, id string) (skills.Descriptor, skills.Content, error) {
	if err := ctx.Err(); err != nil {
		return skills.Descriptor{}, skills.Content{}, err
	}
	if r.getErr != nil {
		return skills.Descriptor{}, skills.Content{}, r.getErr
	}
	for _, skill := range r.skills {
		if normalizeRuntimeSkillID(skill.Descriptor.ID) == normalizeRuntimeSkillID(id) {
			return skill.Descriptor, skill.Content, nil
		}
	}
	return skills.Descriptor{}, skills.Content{}, fmt.Errorf("%w: %s", skills.ErrSkillNotFound, id)
}

func (r *stubSkillsRegistry) Refresh(ctx context.Context) error {
	r.refreshCount++
	if r.refreshFn != nil {
		return r.refreshFn(ctx)
	}
	if r.refreshErr != nil {
		return r.refreshErr
	}
	return ctx.Err()
}

func TestActivateSessionSkillPersistsAndEmitsEvent(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-activate-skill")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	service.SetSkillsRegistry(&stubSkillsRegistry{
		skills: map[string]skills.Skill{
			"go-review": {
				Descriptor: skills.Descriptor{ID: "go_review", Name: "Go Review"},
				Content:    skills.Content{Instruction: "review code"},
			},
		},
	})

	if err := service.ActivateSessionSkill(context.Background(), session.ID, "go_review"); err != nil {
		t.Fatalf("ActivateSessionSkill() error = %v", err)
	}

	loaded := store.sessions[session.ID]
	if got := loaded.ActiveSkillIDs(); len(got) != 1 || got[0] != "go-review" {
		t.Fatalf("expected activated skill persisted, got %+v", got)
	}

	events := collectRuntimeEvents(service.Events())
	if len(events) != 1 || events[0].Type != EventSkillActivated {
		t.Fatalf("expected skill_activated event, got %+v", events)
	}
	payload, ok := events[0].Payload.(SessionSkillEventPayload)
	if !ok || payload.SkillID != "go-review" {
		t.Fatalf("unexpected event payload: %+v", events[0].Payload)
	}
}

func TestActivateSessionSkillRejectsMissingSkill(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-activate-missing")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	service.SetSkillsRegistry(&stubSkillsRegistry{skills: map[string]skills.Skill{}})

	if err := service.ActivateSessionSkill(context.Background(), session.ID, "missing"); err == nil {
		t.Fatalf("expected missing skill activation to fail")
	}
	if got := store.sessions[session.ID].ActiveSkillIDs(); len(got) != 0 {
		t.Fatalf("expected session to remain clean, got %+v", got)
	}
}

func TestActivateSessionSkillRefreshesRegistryOnMissingSkill(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-activate-refresh")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	registry := &stubSkillsRegistry{
		skills: map[string]skills.Skill{},
	}
	registry.refreshFn = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		registry.skills["terminal-diagnosis"] = skills.Skill{
			Descriptor: skills.Descriptor{ID: "terminal-diagnosis", Name: "Terminal Diagnosis"},
			Content:    skills.Content{Instruction: "diagnose"},
		}
		return nil
	}
	service.SetSkillsRegistry(registry)

	if err := service.ActivateSessionSkill(context.Background(), session.ID, "terminal-diagnosis"); err != nil {
		t.Fatalf("ActivateSessionSkill() after refresh error = %v", err)
	}
	if registry.refreshCount != 1 {
		t.Fatalf("Refresh() count = %d, want 1", registry.refreshCount)
	}
	if got := store.sessions[session.ID].ActiveSkillIDs(); len(got) != 1 || got[0] != "terminal-diagnosis" {
		t.Fatalf("expected refreshed skill activated, got %+v", got)
	}
}

func TestActivateSessionSkillReturnsRefreshError(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-activate-refresh-error")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	registry := &stubSkillsRegistry{
		skills:     map[string]skills.Skill{},
		refreshErr: os.ErrPermission,
	}
	service.SetSkillsRegistry(registry)

	err := service.ActivateSessionSkill(context.Background(), session.ID, "terminal-diagnosis")
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("ActivateSessionSkill() error = %v, want %v", err, os.ErrPermission)
	}
	if registry.refreshCount != 1 {
		t.Fatalf("Refresh() count = %d, want 1", registry.refreshCount)
	}
}

func TestActivateSessionSkillValidatesInputAndRegistry(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.ActivateSessionSkill(canceledCtx, "session-id", "go-review"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
	if err := service.ActivateSessionSkill(context.Background(), " ", "go-review"); err == nil {
		t.Fatalf("expected empty session id to fail")
	}
	if err := service.ActivateSessionSkill(context.Background(), "session-id", "go-review"); !errors.Is(err, errSkillsRegistryUnavailable) {
		t.Fatalf("expected registry unavailable error, got %v", err)
	}
}

func TestDeactivateSessionSkillIsIdempotentForUnknownSkill(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-deactivate-skill")
	session.ActivateSkill("go-review")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	if err := service.DeactivateSessionSkill(context.Background(), session.ID, "missing"); err != nil {
		t.Fatalf("DeactivateSessionSkill() error = %v", err)
	}
	if got := store.sessions[session.ID].ActiveSkillIDs(); len(got) != 1 || got[0] != "go-review" {
		t.Fatalf("expected unchanged activations, got %+v", got)
	}
}

func TestDeactivateSessionSkillEmitsEventWhenChanged(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-deactivate-emits")
	session.ActivateSkill("go_review")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	if err := service.DeactivateSessionSkill(context.Background(), session.ID, "GO_REVIEW"); err != nil {
		t.Fatalf("DeactivateSessionSkill() error = %v", err)
	}

	events := collectRuntimeEvents(service.Events())
	if len(events) != 1 || events[0].Type != EventSkillDeactivated {
		t.Fatalf("expected skill_deactivated event, got %+v", events)
	}
	payload, ok := events[0].Payload.(SessionSkillEventPayload)
	if !ok || payload.SkillID != "go-review" {
		t.Fatalf("unexpected event payload: %+v", events[0].Payload)
	}
}

func TestDeactivateSessionSkillValidatesInput(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.DeactivateSessionSkill(canceledCtx, "session-id", "go-review"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
	if err := service.DeactivateSessionSkill(context.Background(), " ", "go-review"); err == nil {
		t.Fatalf("expected empty session id to fail")
	}
}

func TestPrepareTurnSnapshotPassesResolvedSkillsToContextBuilder(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-build-skill")
	session.ActivateSkill("go-review")
	store.sessions[session.ID] = cloneSession(session)

	builder := &stubContextBuilder{}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, builder)
	service.SetSkillsRegistry(&stubSkillsRegistry{
		skills: map[string]skills.Skill{
			"go-review": {
				Descriptor: skills.Descriptor{ID: "go-review", Name: "Go Review"},
				Content:    skills.Content{Instruction: "review code"},
			},
		},
	})

	state := newRunState("run-build-skill", session)
	if _, rebuilt, err := service.prepareTurnBudgetSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("prepareTurnBudgetSnapshot() error = %v", err)
	} else if rebuilt {
		t.Fatalf("did not expect snapshot rebuild")
	}
	if len(builder.lastInput.ActiveSkills) != 1 || builder.lastInput.ActiveSkills[0].Descriptor.ID != "go-review" {
		t.Fatalf("expected active skills forwarded to context builder, got %+v", builder.lastInput.ActiveSkills)
	}
}

func TestPrepareTurnBudgetSnapshotEmitsSkillMissingAndContinues(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-missing-skill")
	session.ActivateSkill("missing-skill")
	store.sessions[session.ID] = cloneSession(session)

	builder := &stubContextBuilder{}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, builder)

	state := newRunState("run-missing-skill", session)
	if _, rebuilt, err := service.prepareTurnBudgetSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("prepareTurnBudgetSnapshot() error = %v", err)
	} else if rebuilt {
		t.Fatalf("did not expect snapshot rebuild")
	}
	if len(builder.lastInput.ActiveSkills) != 0 {
		t.Fatalf("expected missing skill to be skipped, got %+v", builder.lastInput.ActiveSkills)
	}

	events := collectRuntimeEvents(service.Events())
	if len(events) != 1 || events[0].Type != EventSkillMissing {
		t.Fatalf("expected skill_missing event, got %+v", events)
	}
}

func TestPrepareTurnBudgetSnapshotDeduplicatesSkillMissingPerRun(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-missing-skill-dedupe")
	session.ActivateSkill("missing-skill")
	store.sessions[session.ID] = cloneSession(session)

	builder := &stubContextBuilder{}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, builder)

	state := newRunState("run-missing-skill-dedupe", session)
	if _, rebuilt, err := service.prepareTurnBudgetSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("first prepareTurnBudgetSnapshot() error = %v", err)
	} else if rebuilt {
		t.Fatalf("did not expect first snapshot rebuild")
	}
	if _, rebuilt, err := service.prepareTurnBudgetSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("second prepareTurnBudgetSnapshot() error = %v", err)
	} else if rebuilt {
		t.Fatalf("did not expect second snapshot rebuild")
	}

	events := collectRuntimeEvents(service.Events())
	if len(events) != 1 || events[0].Type != EventSkillMissing {
		t.Fatalf("expected exactly one skill_missing event, got %+v", events)
	}
	payload, ok := events[0].Payload.(SessionSkillEventPayload)
	if !ok || payload.SkillID != "missing-skill" {
		t.Fatalf("unexpected event payload: %+v", events[0].Payload)
	}
}

func TestPrepareTurnBudgetSnapshotPropagatesRegistryFailure(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-skill-registry-failure")
	session.ActivateSkill("go-review")
	store.sessions[session.ID] = cloneSession(session)

	builder := &stubContextBuilder{}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, builder)
	service.SetSkillsRegistry(&stubSkillsRegistry{getErr: os.ErrPermission})

	state := newRunState("run-skill-registry-failure", session)
	if _, _, err := service.prepareTurnBudgetSnapshot(context.Background(), &state); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected registry failure to propagate, got %v", err)
	}
	if len(collectRuntimeEvents(service.Events())) != 0 {
		t.Fatalf("expected no skill_missing event on registry failure")
	}
}

func TestListSessionSkillsPropagatesRegistryFailure(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-list-skill-registry-failure")
	session.ActivateSkill("go-review")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	service.SetSkillsRegistry(&stubSkillsRegistry{getErr: os.ErrPermission})

	if _, err := service.ListSessionSkills(context.Background(), session.ID); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected registry failure to propagate, got %v", err)
	}
}

func TestListSessionSkillsHandlesEmptyMissingAndResolvedStates(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

	empty := newRuntimeSession("session-list-empty")
	store.sessions[empty.ID] = cloneSession(empty)
	states, err := service.ListSessionSkills(context.Background(), empty.ID)
	if err != nil {
		t.Fatalf("ListSessionSkills() error = %v", err)
	}
	if states != nil {
		t.Fatalf("expected nil states for empty session, got %+v", states)
	}

	missing := newRuntimeSession("session-list-missing")
	missing.ActivateSkill("missing")
	store.sessions[missing.ID] = cloneSession(missing)
	states, err = service.ListSessionSkills(context.Background(), missing.ID)
	if err != nil {
		t.Fatalf("ListSessionSkills() error = %v", err)
	}
	if len(states) != 1 || !states[0].Missing || states[0].Descriptor != nil {
		t.Fatalf("expected missing state when registry is nil, got %+v", states)
	}

	resolved := newRuntimeSession("session-list-resolved")
	resolved.ActivateSkill("go-review")
	store.sessions[resolved.ID] = cloneSession(resolved)
	service.SetSkillsRegistry(&stubSkillsRegistry{
		skills: map[string]skills.Skill{
			"go-review": {
				Descriptor: skills.Descriptor{ID: "go-review", Name: "Go Review"},
				Content:    skills.Content{Instruction: "review code"},
			},
		},
	})
	states, err = service.ListSessionSkills(context.Background(), resolved.ID)
	if err != nil {
		t.Fatalf("ListSessionSkills() error = %v", err)
	}
	if len(states) != 1 || states[0].Missing || states[0].Descriptor == nil || states[0].Descriptor.ID != "go-review" {
		t.Fatalf("expected resolved descriptor state, got %+v", states)
	}
}

func TestListSessionSkillsValidatesInput(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ListSessionSkills(canceledCtx, "session-id"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
	if _, err := service.ListSessionSkills(context.Background(), " "); err == nil {
		t.Fatalf("expected empty session id to fail")
	}
}

func TestListAvailableSkillsReportsActiveStateAndSorts(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-list-available-skills")
	session.ActivateSkill("go-review")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	service.SetSkillsRegistry(&stubSkillsRegistry{
		skills: map[string]skills.Skill{
			"zeta": {
				Descriptor: skills.Descriptor{ID: "zeta", Name: "Zeta"},
				Content:    skills.Content{Instruction: "z"},
			},
			"go-review": {
				Descriptor: skills.Descriptor{ID: "go-review", Name: "Go Review"},
				Content:    skills.Content{Instruction: "go"},
			},
		},
	})

	states, err := service.ListAvailableSkills(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ListAvailableSkills() error = %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("ListAvailableSkills() len = %d, want 2", len(states))
	}
	if states[0].Descriptor.ID != "go-review" || !states[0].Active {
		t.Fatalf("expected go-review active first, got %+v", states[0])
	}
	if states[1].Descriptor.ID != "zeta" || states[1].Active {
		t.Fatalf("expected zeta inactive second, got %+v", states[1])
	}
}

func TestListAvailableSkillsUsesConfigWorkdirWhenSessionIsEmpty(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	registry := &stubSkillsRegistry{
		listFilterByWS: true,
		skills: map[string]skills.Skill{
			"workspace-only": {
				Descriptor: skills.Descriptor{
					ID:    "workspace-only",
					Name:  "Workspace Only",
					Scope: skills.ScopeWorkspace,
				},
				Content: skills.Content{Instruction: "workspace"},
			},
		},
	}
	service.SetSkillsRegistry(registry)

	states, err := service.ListAvailableSkills(context.Background(), "")
	if err != nil {
		t.Fatalf("ListAvailableSkills() error = %v", err)
	}
	if len(states) != 1 || states[0].Descriptor.ID != "workspace-only" {
		t.Fatalf("expected workspace skill visible with config workdir fallback, got %+v", states)
	}
	if strings.TrimSpace(registry.lastListInput.Workspace) == "" {
		t.Fatalf("expected non-empty workspace fallback, got %+v", registry.lastListInput)
	}
}

func TestListAvailableSkillsHandlesValidationAndRegistryErrors(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-list-available-errors")
	store.sessions[session.ID] = cloneSession(session)
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ListAvailableSkills(canceledCtx, session.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
	if _, err := service.ListAvailableSkills(context.Background(), session.ID); !errors.Is(err, errSkillsRegistryUnavailable) {
		t.Fatalf("expected registry unavailable error, got %v", err)
	}

	service.SetSkillsRegistry(&stubSkillsRegistry{getErr: os.ErrPermission})
	if _, err := service.ListAvailableSkills(context.Background(), "missing-session"); err == nil {
		t.Fatalf("expected missing session error")
	}
}

func TestPrioritizeToolSpecsBySkillHintsOnlyReordersVisibleTools(t *testing.T) {
	t.Parallel()

	specs := []providertypes.ToolSpec{
		{Name: "filesystem_read_file"},
		{Name: "bash"},
		{Name: "webfetch"},
	}
	activeSkills := []skills.Skill{
		{
			Descriptor: skills.Descriptor{ID: "go-review", Name: "Go Review"},
			Content: skills.Content{
				Instruction: "review",
				ToolHints:   []string{"webfetch", "unknown_tool", "bash"},
			},
		},
	}

	prioritized := prioritizeToolSpecsBySkillHints(specs, activeSkills)
	got := []string{prioritized[0].Name, prioritized[1].Name, prioritized[2].Name}
	want := []string{"webfetch", "bash", "filesystem_read_file"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prioritized tool order = %v, want %v", got, want)
	}
}

func TestPrioritizeToolSpecsBySkillHintsKeepsNonHintedRelativeOrder(t *testing.T) {
	t.Parallel()

	specs := []providertypes.ToolSpec{
		{Name: "filesystem_read_file"},
		{Name: "webfetch"},
		{Name: "bash"},
		{Name: "mcp_tool"},
	}
	activeSkills := []skills.Skill{
		{
			Descriptor: skills.Descriptor{ID: "go-review", Name: "Go Review"},
			Content: skills.Content{
				Instruction: "review",
				ToolHints:   []string{"bash"},
			},
		},
	}

	prioritized := prioritizeToolSpecsBySkillHints(specs, activeSkills)
	got := []string{prioritized[0].Name, prioritized[1].Name, prioritized[2].Name, prioritized[3].Name}
	want := []string{"bash", "filesystem_read_file", "webfetch", "mcp_tool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prioritized tool order = %v, want %v", got, want)
	}
}

func TestPrepareTurnSnapshotPrioritizesToolsByActiveSkillHints(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-skill-tool-priority")
	session.ActivateSkill("go-review")
	store.sessions[session.ID] = cloneSession(session)

	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "filesystem_read_file"},
			{Name: "bash"},
		},
	}
	service := NewWithFactory(manager, toolManager, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	service.SetSkillsRegistry(&stubSkillsRegistry{
		skills: map[string]skills.Skill{
			"go-review": {
				Descriptor: skills.Descriptor{ID: "go-review", Name: "Go Review"},
				Content: skills.Content{
					Instruction: "review",
					ToolHints:   []string{"bash"},
				},
			},
		},
	})

	state := newRunState("run-skill-tool-priority", session)
	snapshot, rebuilt, err := service.prepareTurnBudgetSnapshot(context.Background(), &state)
	if err != nil {
		t.Fatalf("prepareTurnBudgetSnapshot() error = %v", err)
	}
	if rebuilt {
		t.Fatalf("did not expect snapshot rebuild")
	}
	if len(snapshot.Request.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(snapshot.Request.Tools))
	}
	if snapshot.Request.Tools[0].Name != "bash" {
		t.Fatalf("expected hinted tool first, got %q", snapshot.Request.Tools[0].Name)
	}
}

func TestMutateSessionSkillsCoversValidationAndSaveFailure(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	baseStore := newMemoryStore()
	session := newRuntimeSession("session-mutate-branches")
	baseStore.sessions[session.ID] = cloneSession(session)
	service := NewWithFactory(manager, &stubToolManager{}, baseStore, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

	if _, _, err := service.mutateSessionSkills(context.Background(), session.ID, nil); err == nil {
		t.Fatalf("expected nil mutate function to fail")
	}

	failing := &failingStore{Store: baseStore, saveErr: errors.New("save failed"), failOnSave: 1, ignoreContextErr: true}
	service.sessionStore = failing
	if _, _, err := service.mutateSessionSkills(context.Background(), session.ID, func(current *agentsession.Session) bool {
		return current.ActivateSkill("go-review")
	}); err == nil {
		t.Fatalf("expected save failure to propagate")
	}
}

func TestEmitSkillMissingOnceHandlesNilStateAndDedup(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

	service.emitSkillMissingOnce(context.Background(), nil, "missing-nil-state")
	state := newRunState("run-missing-once", newRuntimeSession("session-missing-once"))
	service.emitSkillMissingOnce(context.Background(), &state, "go_review")
	service.emitSkillMissingOnce(context.Background(), &state, "go-review")

	events := collectRuntimeEvents(service.Events())
	if len(events) != 2 {
		t.Fatalf("expected one nil-state event and one deduped run-state event, got %+v", events)
	}
}

func TestNormalizeRuntimeSkillID(t *testing.T) {
	t.Parallel()

	if got := normalizeRuntimeSkillID("  Go_Review  "); got != "go-review" {
		t.Fatalf("unexpected normalized id: %q", got)
	}
	if got := normalizeRuntimeSkillID(" -  "); got != "" {
		t.Fatalf("expected blank normalized id, got %q", got)
	}
}

func TestResolveActiveSkillsBranchCoverage(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-resolve-active-skills")
	session.ActivateSkill("missing-a")
	session.ActivateSkill("missing-b")
	store.sessions[session.ID] = cloneSession(session)
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.resolveActiveSkills(canceledCtx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context to fail early, got %v", err)
	}
	if skillsResolved, err := service.resolveActiveSkills(context.Background(), nil); err != nil || skillsResolved != nil {
		t.Fatalf("expected nil state to return nil,nil; got %+v err=%v", skillsResolved, err)
	}

	state := newRunState("run-resolve-active-skills", session)
	skillsResolved, err := service.resolveActiveSkills(context.Background(), &state)
	if err != nil {
		t.Fatalf("resolveActiveSkills() error = %v", err)
	}
	if len(skillsResolved) != 0 {
		t.Fatalf("expected unresolved skills with nil registry, got %+v", skillsResolved)
	}

	events := collectRuntimeEvents(service.Events())
	if len(events) != 2 {
		t.Fatalf("expected two skill_missing events, got %+v", events)
	}
}

func TestListSessionSkillsHandlesSkillNotFoundFromRegistry(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-list-session-skills-missing")
	session.ActivateSkill("missing-skill")
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	service.SetSkillsRegistry(&stubSkillsRegistry{skills: map[string]skills.Skill{}})

	states, err := service.ListSessionSkills(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ListSessionSkills() error = %v", err)
	}
	if len(states) != 1 || !states[0].Missing || states[0].Descriptor != nil {
		t.Fatalf("expected skill-not-found to map to missing state, got %+v", states)
	}
}

func TestListAvailableSkillsAdditionalBranches(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-list-available-branches")
	session.Workdir = "/tmp/project"
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, &stubContextBuilder{})
	registry := &stubSkillsRegistry{skills: map[string]skills.Skill{}}
	service.SetSkillsRegistry(registry)

	states, err := service.ListAvailableSkills(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ListAvailableSkills() error = %v", err)
	}
	if states != nil {
		t.Fatalf("expected nil states for empty descriptor list, got %+v", states)
	}
	if strings.TrimSpace(registry.lastListInput.Workspace) == "" {
		t.Fatalf("expected workspace from session/config, got %+v", registry.lastListInput)
	}

	service.configManager = nil
	if _, err := service.ListAvailableSkills(context.Background(), session.ID); err != nil {
		t.Fatalf("expected config-manager-nil branch to still succeed, got %v", err)
	}
	if strings.TrimSpace(registry.lastListInput.Workspace) != "/tmp/project" {
		t.Fatalf("expected workspace from session workdir when config manager nil, got %+v", registry.lastListInput)
	}
}

func TestSkillHelperFunctionsBranches(t *testing.T) {
	t.Parallel()

	if set := skillSetFromIDs(nil); len(set) != 0 {
		t.Fatalf("expected empty set for nil input, got %+v", set)
	}
	set := skillSetFromIDs([]string{"  ", "Go_Review", "go-review"})
	if len(set) != 1 {
		t.Fatalf("expected deduped set size 1, got %+v", set)
	}
	if _, ok := set["go-review"]; !ok {
		t.Fatalf("expected normalized key in set, got %+v", set)
	}

	hints := collectSkillToolHints([]skills.Skill{
		{
			Content: skills.Content{ToolHints: []string{"", "bash", " Bash ", "web_fetch"}},
		},
		{
			Content: skills.Content{ToolHints: []string{"web-fetch"}},
		},
	})
	if !reflect.DeepEqual(hints, []string{"bash", "web-fetch"}) {
		t.Fatalf("unexpected normalized hints: %+v", hints)
	}
	if collectSkillToolHints(nil) != nil {
		t.Fatalf("expected nil for empty active skills")
	}
}
