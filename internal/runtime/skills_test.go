package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
)

type stubSkillsRegistry struct {
	skills map[string]skills.Skill
	getErr error
}

func (r *stubSkillsRegistry) List(ctx context.Context, input skills.ListInput) ([]skills.Descriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]skills.Descriptor, 0, len(r.skills))
	for _, skill := range r.skills {
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
	if _, rebuilt, err := service.prepareTurnSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("prepareTurnSnapshot() error = %v", err)
	} else if rebuilt {
		t.Fatalf("did not expect snapshot rebuild")
	}
	if len(builder.lastInput.ActiveSkills) != 1 || builder.lastInput.ActiveSkills[0].Descriptor.ID != "go-review" {
		t.Fatalf("expected active skills forwarded to context builder, got %+v", builder.lastInput.ActiveSkills)
	}
}

func TestPrepareTurnSnapshotEmitsSkillMissingAndContinues(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-missing-skill")
	session.ActivateSkill("missing-skill")
	store.sessions[session.ID] = cloneSession(session)

	builder := &stubContextBuilder{}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, builder)

	state := newRunState("run-missing-skill", session)
	if _, rebuilt, err := service.prepareTurnSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("prepareTurnSnapshot() error = %v", err)
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

func TestPrepareTurnSnapshotDeduplicatesSkillMissingPerRun(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newRuntimeSession("session-missing-skill-dedupe")
	session.ActivateSkill("missing-skill")
	store.sessions[session.ID] = cloneSession(session)

	builder := &stubContextBuilder{}
	service := NewWithFactory(manager, &stubToolManager{}, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, builder)

	state := newRunState("run-missing-skill-dedupe", session)
	if _, rebuilt, err := service.prepareTurnSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("first prepareTurnSnapshot() error = %v", err)
	} else if rebuilt {
		t.Fatalf("did not expect first snapshot rebuild")
	}
	if _, rebuilt, err := service.prepareTurnSnapshot(context.Background(), &state); err != nil {
		t.Fatalf("second prepareTurnSnapshot() error = %v", err)
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

func TestPrepareTurnSnapshotPropagatesRegistryFailure(t *testing.T) {
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
	if _, _, err := service.prepareTurnSnapshot(context.Background(), &state); !errors.Is(err, os.ErrPermission) {
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

func TestServiceRunReinjectsSkillsAfterAutoCompact(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Context.AutoCompact.Enabled = true
		cfg.Context.AutoCompact.InputTokenThreshold = 1
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	store := newMemoryStore()
	session := newRuntimeSession("session-auto-compact-skills")
	session.ActivateSkill("go-review")
	session.TokenInputTotal = 3
	store.sessions[session.ID] = cloneSession(session)

	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt:         "prompt",
				Messages:             append([]providertypes.Message(nil), input.Messages...),
				AutoCompactSuggested: input.Metadata.SessionInputTokens >= 1,
			}, nil
		},
	}
	compactRunner := &stubCompactRunner{
		runFn: func(ctx context.Context, input contextcompact.Input) (contextcompact.Result, error) {
			return contextcompact.Result{
				Applied:   true,
				Messages:  append([]providertypes.Message(nil), input.Messages...),
				TaskState: input.TaskState.Clone(),
				Metrics: contextcompact.Metrics{
					TriggerMode: string(contextcompact.ModeAuto),
				},
			}, nil
		},
	}
	scripted := &scriptedProvider{
		streams: [][]providertypes.StreamEvent{
			{providertypes.NewTextDeltaStreamEvent("done")},
		},
	}
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})

	service := NewWithFactory(manager, registry, store, &scriptedProviderFactory{provider: scripted}, builder)
	service.compactRunner = compactRunner
	service.SetSkillsRegistry(&stubSkillsRegistry{
		skills: map[string]skills.Skill{
			"go-review": {
				Descriptor: skills.Descriptor{ID: "go-review", Name: "Go Review"},
				Content:    skills.Content{Instruction: "review code"},
			},
		},
	})

	if err := service.Run(context.Background(), UserInput{SessionID: session.ID, RunID: "run-auto-compact-skills", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(builder.builds) < 2 {
		t.Fatalf("expected context builder to run before and after compact, got %d", len(builder.builds))
	}
	for idx, build := range builder.builds[:2] {
		if len(build.ActiveSkills) != 1 || build.ActiveSkills[0].Descriptor.ID != "go-review" {
			t.Fatalf("expected active skill on build %d, got %+v", idx, build.ActiveSkills)
		}
	}
}
