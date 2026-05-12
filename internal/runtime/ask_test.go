package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/skills"
)

// ---- stub for skills.Registry used in buildAskSystemPrompt tests ----

type stubAskSkillsRegistry struct {
	skills map[string]stubAskSkill
	getErr error
}

type stubAskSkill struct {
	descriptor  skills.Descriptor
	instruction string
}

func (r *stubAskSkillsRegistry) List(_ context.Context, _ skills.ListInput) ([]skills.Descriptor, error) {
	return nil, nil
}
func (r *stubAskSkillsRegistry) Get(_ context.Context, id string) (skills.Descriptor, skills.Content, error) {
	if r.getErr != nil {
		return skills.Descriptor{}, skills.Content{}, r.getErr
	}
	sk, ok := r.skills[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return skills.Descriptor{}, skills.Content{}, errors.New("skill not found")
	}
	return sk.descriptor, skills.Content{Instruction: sk.instruction}, nil
}
func (r *stubAskSkillsRegistry) Refresh(_ context.Context) error { return nil }
func (r *stubAskSkillsRegistry) Reload(_ context.Context) error  { return nil }
func (r *stubAskSkillsRegistry) Count() int                      { return 0 }

func newStubAskSkillsRegistry() *stubAskSkillsRegistry {
	return &stubAskSkillsRegistry{skills: make(map[string]stubAskSkill)}
}

func (r *stubAskSkillsRegistry) addSkill(id, instruction string) {
	r.skills[strings.ToLower(strings.TrimSpace(id))] = stubAskSkill{
		descriptor:  skills.Descriptor{ID: strings.TrimSpace(id)},
		instruction: instruction,
	}
}

// ---- Ask nil / cancelled / empty input ----

func TestAskNilService(t *testing.T) {
	var s *Service
	err := s.Ask(context.Background(), AskInput{UserQuery: "test"})
	if err == nil || !strings.Contains(err.Error(), "service is nil") {
		t.Fatalf("expected nil service error, got %v", err)
	}
}

func TestAskCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Service{}
	err := s.Ask(ctx, AskInput{UserQuery: "test"})
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestAskEmptyUserQuery(t *testing.T) {
	s := &Service{}
	err := s.Ask(context.Background(), AskInput{UserQuery: "  "})
	if err == nil || !strings.Contains(err.Error(), "user query is empty") {
		t.Fatalf("expected empty query error, got %v", err)
	}
}

// ---- DeleteAskSession ----

func TestDeleteAskSessionNilService(t *testing.T) {
	var s *Service
	_, err := s.DeleteAskSession(context.Background(), DeleteAskSessionInput{SessionID: "s1"})
	if err == nil || !strings.Contains(err.Error(), "service is nil") {
		t.Fatalf("expected nil service error, got %v", err)
	}
}

func TestDeleteAskSessionEmptyID(t *testing.T) {
	s := &Service{}
	ok, err := s.DeleteAskSession(context.Background(), DeleteAskSessionInput{SessionID: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false for empty session ID")
	}
}

func TestDeleteAskSessionNoStore(t *testing.T) {
	s := &Service{}
	ok, err := s.DeleteAskSession(context.Background(), DeleteAskSessionInput{SessionID: "s1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false when store is nil")
	}
}

func TestDeleteAskSessionRemovesSession(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	_ = store.Save(context.Background(), AskSession{ID: "s1"})

	s := &Service{askStore: store}
	ok, err := s.DeleteAskSession(context.Background(), DeleteAskSessionInput{SessionID: "s1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true when session was deleted")
	}
	_, loaded, _ := store.Load(context.Background(), "s1")
	if loaded {
		t.Fatal("session should have been deleted")
	}
}

// ---- AskSession.Clone() ----

func TestAskSessionCloneDeepCopy(t *testing.T) {
	original := AskSession{
		ID:      "s1",
		Workdir: "/tmp",
		Skills:  []string{"skill-a"},
		Messages: []AskMessage{
			{Role: "user", Content: "hello"},
		},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	cloned := original.Clone()

	cloned.Skills[0] = "skill-b"
	cloned.Messages[0].Content = "modified"

	if original.Skills[0] != "skill-a" {
		t.Fatal("original skills should not be affected by clone mutation")
	}
	if original.Messages[0].Content != "hello" {
		t.Fatal("original messages should not be affected by clone mutation")
	}
	if cloned.ID != "s1" || cloned.Workdir != "/tmp" {
		t.Fatalf("clone fields mismatch: ID=%q Workdir=%q", cloned.ID, cloned.Workdir)
	}
}

func TestCloneZeroSession(t *testing.T) {
	cloned := AskSession{}.Clone()
	if len(cloned.Skills) != 0 {
		t.Fatalf("cloned skills should be empty, got %#v", cloned.Skills)
	}
	if len(cloned.Messages) != 0 {
		t.Fatalf("cloned messages should be empty, got %#v", cloned.Messages)
	}
	if cloned.ID != "" {
		t.Fatalf("cloned ID should be empty, got %q", cloned.ID)
	}
}

// ---- normalizeAskMessageRole ----

func TestNormalizeAskMessageRole(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"user", "user"},
		{"USER", "user"},
		{"  User  ", "user"},
		{"assistant", "assistant"},
		{"ASSISTANT", "assistant"},
		{"system", "assistant"},
		{"", "assistant"},
		{"unknown", "assistant"},
	}
	for _, tt := range tests {
		got := normalizeAskMessageRole(tt.input)
		if got != tt.want {
			t.Errorf("normalizeAskMessageRole(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---- ID generation ----

func TestGenerateAskSessionID(t *testing.T) {
	s := &Service{}
	id1 := s.generateAskSessionID()
	id2 := s.generateAskSessionID()
	if id1 == id2 {
		t.Fatal("session IDs should be unique")
	}
	if !strings.HasPrefix(id1, "ask-") {
		t.Fatalf("session ID should start with 'ask-', got %q", id1)
	}
	pidStr := fmt.Sprintf("-%d-", os.Getpid())
	if !strings.Contains(id1, pidStr) {
		t.Fatalf("session ID should contain PID, got %q", id1)
	}
}

func TestGenerateAskRunID(t *testing.T) {
	s := &Service{}
	id1 := s.generateAskRunID()
	id2 := s.generateAskRunID()
	if id1 == id2 {
		t.Fatal("run IDs should be unique")
	}
	if !strings.HasPrefix(id1, "ask-run-") {
		t.Fatalf("run ID should start with 'ask-run-', got %q", id1)
	}
}

// ---- resolveAskPromptConfig ----

func TestResolveAskPromptConfigUsesDefaultsWhenNilService(t *testing.T) {
	var s *Service
	got := s.resolveAskPromptConfig()
	if got.MaxInputTokens != config.DefaultAskMaxInputTokens {
		t.Fatalf("MaxInputTokens = %d, want %d", got.MaxInputTokens, config.DefaultAskMaxInputTokens)
	}
	if got.RetainTurns != config.DefaultAskRetainTurns {
		t.Fatalf("RetainTurns = %d, want %d", got.RetainTurns, config.DefaultAskRetainTurns)
	}
	if got.SummaryMaxChars != config.DefaultAskSummaryMaxChars {
		t.Fatalf("SummaryMaxChars = %d, want %d", got.SummaryMaxChars, config.DefaultAskSummaryMaxChars)
	}
}

func TestResolveAskPromptConfigUsesDefaultsWithNoConfigManager(t *testing.T) {
	s := &Service{}
	got := s.resolveAskPromptConfig()
	if got.MaxInputTokens != config.DefaultAskMaxInputTokens {
		t.Fatalf("MaxInputTokens = %d, want %d", got.MaxInputTokens, config.DefaultAskMaxInputTokens)
	}
	if got.RetainTurns != config.DefaultAskRetainTurns {
		t.Fatalf("RetainTurns = %d, want %d", got.RetainTurns, config.DefaultAskRetainTurns)
	}
	if got.SummaryMaxChars != config.DefaultAskSummaryMaxChars {
		t.Fatalf("SummaryMaxChars = %d, want %d", got.SummaryMaxChars, config.DefaultAskSummaryMaxChars)
	}
}

func TestResolveAskPromptConfigMergesOverrides(t *testing.T) {
	cm := newRuntimeConfigManager(t)
	s := &Service{configManager: cm}
	got := s.resolveAskPromptConfig()
	// With newRuntimeConfigManager, the default Ask config values should be picked up
	if got.MaxInputTokens != config.DefaultAskMaxInputTokens {
		t.Fatalf("MaxInputTokens = %d, want %d", got.MaxInputTokens, config.DefaultAskMaxInputTokens)
	}
	if got.RetainTurns != config.DefaultAskRetainTurns {
		t.Fatalf("RetainTurns = %d, want %d", got.RetainTurns, config.DefaultAskRetainTurns)
	}
	if got.SummaryMaxChars != config.DefaultAskSummaryMaxChars {
		t.Fatalf("SummaryMaxChars = %d, want %d", got.SummaryMaxChars, config.DefaultAskSummaryMaxChars)
	}
}

// ---- normalizeAskSkillIDs ----

func TestNormalizeAskSkillIDs(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{"nil", nil, nil},
		{"empty", []string{}, nil},
		{"single", []string{"skill-a"}, []string{"skill-a"}},
		{"trim whitespace", []string{"  skill-a  "}, []string{"skill-a"}},
		{"dedup exact", []string{"skill-a", "skill-a"}, []string{"skill-a"}},
		{"dedup case insensitive", []string{"Skill-A", "skill-a"}, []string{"Skill-A"}},
		{"skip empty string", []string{"", "skill-a", ""}, []string{"skill-a"}},
		{"multiple skills", []string{"skill-a", "skill-b"}, []string{"skill-a", "skill-b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAskSkillIDs(tt.input)
			if len(got) != len(tt.expect) {
				t.Fatalf("len = %d, want %d: %#v", len(got), len(tt.expect), got)
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Fatalf("[%d] = %q, want %q", i, got[i], tt.expect[i])
				}
			}
		})
	}
}

// ---- askMessagesToTurns ----

func TestAskMessagesToTurns(t *testing.T) {
	tests := []struct {
		name     string
		messages []AskMessage
		want     int
	}{
		{"nil", nil, 0},
		{"empty", []AskMessage{}, 0},
		{"single user", []AskMessage{{Role: "user", Content: "q1"}}, 1},
		{"user+assistant", []AskMessage{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
		}, 1},
		{"two turns", []AskMessage{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "q2"},
			{Role: "assistant", Content: "a2"},
		}, 2},
		{"assistant without user ignored", []AskMessage{
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "q1"},
		}, 1},
		{"trailing user", []AskMessage{
			{Role: "user", Content: "q1"},
			{Role: "user", Content: "q2"},
		}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turns := askMessagesToTurns(tt.messages)
			if len(turns) != tt.want {
				t.Fatalf("len = %d, want %d: %#v", len(turns), tt.want, turns)
			}
		})
	}
}

func TestAskMessagesToTurnsContent(t *testing.T) {
	messages := []AskMessage{
		{Role: "user", Content: "what is go?"},
		{Role: "assistant", Content: "Go is a language."},
	}
	turns := askMessagesToTurns(messages)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].UserQuery != "what is go?" {
		t.Fatalf("UserQuery = %q", turns[0].UserQuery)
	}
	if turns[0].Assistant != "Go is a language." {
		t.Fatalf("Assistant = %q", turns[0].Assistant)
	}
}

// ---- compactAskSessionMessages ----

func TestCompactAskSessionMessagesSummaryOnly(t *testing.T) {
	result := agentcontext.AskPromptBuildResult{
		Summary: "previous conversations about Go",
	}
	messages := compactAskSessionMessages(result)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "之前对话摘要" {
		t.Fatalf("first message should be summary marker, got %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "previous conversations about Go" {
		t.Fatalf("second message should be summary, got %+v", messages[1])
	}
}

func TestCompactAskSessionMessagesEmptySummary(t *testing.T) {
	result := agentcontext.AskPromptBuildResult{
		Summary: "  ",
	}
	messages := compactAskSessionMessages(result)
	if len(messages) != 0 {
		t.Fatalf("expected 0 messages for empty summary, got %d", len(messages))
	}
}

func TestCompactAskSessionMessagesRetainedTurns(t *testing.T) {
	result := agentcontext.AskPromptBuildResult{
		RetainedTurns: []agentcontext.AskTurn{
			{UserQuery: "q1", Assistant: "a1"},
			{UserQuery: "q2"},
		},
	}
	messages := compactAskSessionMessages(result)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(messages), messages)
	}
	if messages[0].Role != "user" || messages[0].Content != "q1" {
		t.Fatalf("unexpected messages[0]: %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "a1" {
		t.Fatalf("unexpected messages[1]: %+v", messages[1])
	}
	if messages[2].Role != "user" || messages[2].Content != "q2" {
		t.Fatalf("unexpected messages[2]: %+v", messages[2])
	}
}

func TestCompactAskSessionMessagesSkipsEmptyQuery(t *testing.T) {
	result := agentcontext.AskPromptBuildResult{
		RetainedTurns: []agentcontext.AskTurn{
			{UserQuery: "  ", Assistant: "a1"},
			{UserQuery: "q1", Assistant: "a1"},
		},
	}
	messages := compactAskSessionMessages(result)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
}

func TestCompactAskSessionMessagesBothSummaryAndTurns(t *testing.T) {
	result := agentcontext.AskPromptBuildResult{
		Summary: "summary text",
		RetainedTurns: []agentcontext.AskTurn{
			{UserQuery: "q1", Assistant: "a1"},
		},
	}
	messages := compactAskSessionMessages(result)
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}
}

// ---- appendAskMessage ----

func TestAppendAskMessageEmptyContent(t *testing.T) {
	session := AskSession{}
	result := appendAskMessage(session, "user", "  ")
	if len(result.Messages) != 0 {
		t.Fatal("expected no messages for empty content")
	}
}

func TestAppendAskMessageNormalizesRole(t *testing.T) {
	session := AskSession{}
	result := appendAskMessage(session, "USER", "hello")
	if result.Messages[0].Role != "user" {
		t.Fatalf("role = %q, want 'user'", result.Messages[0].Role)
	}
}

func TestAppendAskMessageTruncatesExcessTurns(t *testing.T) {
	session := AskSession{}
	for i := 0; i < askSessionMaxTurns+2; i++ {
		session = appendAskMessage(session, "user", fmt.Sprintf("q%d", i))
		session = appendAskMessage(session, "assistant", fmt.Sprintf("a%d", i))
	}
	turns := askMessagesToTurns(session.Messages)
	if len(turns) > askSessionMaxTurns {
		t.Fatalf("turns = %d, want <= %d", len(turns), askSessionMaxTurns)
	}
}

// ---- buildAskSystemPrompt ----

func TestBuildAskSystemPromptNoSkills(t *testing.T) {
	got := buildAskSystemPrompt(context.Background(), nil, nil)
	if !strings.Contains(got, "NeoCode Ask mode assistant") {
		t.Fatalf("unexpected system prompt: %q", got)
	}
	if strings.Contains(got, "Skill") {
		t.Fatalf("should not contain Skill section, got %q", got)
	}
}

func TestBuildAskSystemPromptEmptySkillIDs(t *testing.T) {
	registry := newStubAskSkillsRegistry()
	got := buildAskSystemPrompt(context.Background(), registry, []string{})
	if !strings.Contains(got, "NeoCode Ask mode assistant") {
		t.Fatalf("unexpected system prompt: %q", got)
	}
	if strings.Contains(got, "Skill") {
		t.Fatalf("should not contain Skill section, got %q", got)
	}
}

func TestBuildAskSystemPromptWithSkills(t *testing.T) {
	registry := newStubAskSkillsRegistry()
	registry.addSkill("s1", "do X")
	registry.addSkill("s2", "do Y")

	got := buildAskSystemPrompt(context.Background(), registry, []string{"  s1  ", "s2"})
	if !strings.Contains(got, "Skill `s1`") {
		t.Fatalf("expected Skill s1, got %q", got)
	}
	if !strings.Contains(got, "do X") {
		t.Fatalf("expected do X, got %q", got)
	}
	if !strings.Contains(got, "Skill `s2`") {
		t.Fatalf("expected Skill s2, got %q", got)
	}
}

func TestBuildAskSystemPromptDedupSkills(t *testing.T) {
	registry := newStubAskSkillsRegistry()
	registry.addSkill("s1", "do X")

	got := buildAskSystemPrompt(context.Background(), registry, []string{"s1", "S1", "  S1  "})
	count := strings.Count(got, "Skill `s1`")
	if count != 1 {
		t.Fatalf("Skill s1 should appear once, got %d occurrences in:\n%s", count, got)
	}
}

func TestBuildAskSystemPromptSkillNotFound(t *testing.T) {
	registry := newStubAskSkillsRegistry()
	got := buildAskSystemPrompt(context.Background(), registry, []string{"nonexistent"})
	if strings.Contains(got, "Skill") {
		t.Fatalf("should not contain Skill section for missing skill, got %q", got)
	}
}

func TestBuildAskSystemPromptSkillEmptyInstruction(t *testing.T) {
	registry := newStubAskSkillsRegistry()
	registry.addSkill("s1", "  ")

	got := buildAskSystemPrompt(context.Background(), registry, []string{"s1"})
	if strings.Contains(got, "Skill `s1`") {
		t.Fatalf("should not contain Skill section for empty instruction, got %q", got)
	}
}

func TestBuildAskSystemPromptEmptySkillID(t *testing.T) {
	registry := newStubAskSkillsRegistry()
	registry.addSkill("s1", "do X")

	got := buildAskSystemPrompt(context.Background(), registry, []string{"", "  "})
	if strings.Contains(got, "Skill") {
		t.Fatalf("should not contain Skill section for empty IDs, got %q", got)
	}
}

func TestBuildAskSystemPromptNilRegistry(t *testing.T) {
	got := buildAskSystemPrompt(context.Background(), nil, []string{"s1"})
	if !strings.Contains(got, "NeoCode Ask mode assistant") {
		t.Fatalf("unexpected system prompt: %q", got)
	}
}

// ---- mapAskErrorCode ----

func TestMapAskErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "INTERNAL_ERROR"},
		{"deadline", context.DeadlineExceeded, "TIMEOUT"},
		{"canceled", context.Canceled, "CANCELED"},
		{"rate limit", &provider.ProviderError{Code: provider.ErrorCodeRateLimit}, "RATE_LIMITED"},
		{"wrapped rate limit", fmt.Errorf("wrap: %w", &provider.ProviderError{Code: provider.ErrorCodeRateLimit}), "RATE_LIMITED"},
		{"timeout provider", &provider.ProviderError{Code: provider.ErrorCodeTimeout}, "TIMEOUT"},
		{"server error", &provider.ProviderError{Code: provider.ErrorCodeServer}, "PROVIDER_ERROR"},
		{"client error", &provider.ProviderError{Code: provider.ErrorCodeClient}, "PROVIDER_ERROR"},
		{"generic", errors.New("something"), "INTERNAL_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapAskErrorCode(tt.err)
			if got != tt.want {
				t.Fatalf("mapAskErrorCode() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---- emitAskError ----

func TestEmitAskErrorNilService(t *testing.T) {
	var s *Service
	s.emitAskError(context.Background(), "r1", "s1", errors.New("test"))
}

func TestEmitAskErrorNilError(t *testing.T) {
	s := &Service{}
	s.emitAskError(context.Background(), "r1", "s1", nil)
}

func TestEmitAskErrorWritesEvent(t *testing.T) {
	cm := newRuntimeConfigManager(t)
	ch := make(chan RuntimeEvent, 16)
	s := &Service{
		configManager: cm,
		events:        ch,
	}

	s.emitAskError(context.Background(), "r1", "s1", fmt.Errorf("test error: %w", &provider.ProviderError{
		Code:    provider.ErrorCodeRateLimit,
		Message: "too many requests",
	}))

	select {
	case event := <-ch:
		if event.Type != EventError {
			t.Fatalf("expected EventError, got %s", event.Type)
		}
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			t.Fatal("payload should be map[string]any")
		}
		if payload["code"] != "RATE_LIMITED" {
			t.Fatalf("code = %v, want RATE_LIMITED", payload["code"])
		}
		if !strings.Contains(payload["message"].(string), "too many requests") {
			t.Fatalf("message = %v", payload["message"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// ---- captureAskRuntimeEvent ----

func TestCaptureAskRuntimeEvent(t *testing.T) {
	s := &Service{}
	s.captureAskRuntimeEvent(RuntimeEvent{Type: "test"})
}

// ---- streamGenerateResult ----

func TestStreamGenerateResultSuccess(t *testing.T) {
	textResult := providertypes.Message{
		Role: providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart("hello world"),
		},
	}
	outcome := streamGenerateResult{
		message:      textResult,
		inputTokens:  10,
		outputTokens: 5,
		err:          nil,
	}
	if outcome.err != nil {
		t.Fatalf("unexpected error: %v", outcome.err)
	}
	if outcome.inputTokens != 10 || outcome.outputTokens != 5 {
		t.Fatalf("unexpected token counts: %d/%d", outcome.inputTokens, outcome.outputTokens)
	}
	if outcome.message.Role != providertypes.RoleAssistant {
		t.Fatalf("unexpected role: %s", outcome.message.Role)
	}
}

func TestStreamGenerateResultWithError(t *testing.T) {
	outcome := streamGenerateResult{
		err: errors.New("stream failed"),
	}
	if outcome.err == nil {
		t.Fatal("expected error")
	}
	if outcome.inputTokens != 0 || outcome.outputTokens != 0 {
		t.Fatal("expected zero tokens for failed outcome")
	}
}

func TestStreamGenerateResultObservedFlags(t *testing.T) {
	outcome := streamGenerateResult{
		inputTokens:    5,
		outputTokens:   3,
		inputObserved:  true,
		outputObserved: true,
	}
	if !outcome.inputObserved || !outcome.outputObserved {
		t.Fatal("expected observed flags to be true")
	}
}

// ---- inMemoryAskSessionStore ----

func TestInMemoryAskSessionStoreLoadNotExists(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	_, loaded, err := store.Load(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded {
		t.Fatal("expected false for nonexistent session")
	}
}

func TestInMemoryAskSessionStoreLoadEmptyID(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	_, loaded, err := store.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded {
		t.Fatal("expected false for empty ID")
	}
}

func TestInMemoryAskSessionStoreSaveAndLoad(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	session := AskSession{
		ID:      "s1",
		Workdir: "/tmp/test",
		Skills:  []string{"skill-a"},
		Messages: []AskMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		},
	}
	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, found, err := store.Load(context.Background(), "s1")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !found {
		t.Fatal("expected session to be found")
	}
	if loaded.ID != "s1" {
		t.Fatalf("ID = %q", loaded.ID)
	}
	if loaded.Workdir != "/tmp/test" {
		t.Fatalf("Workdir = %q", loaded.Workdir)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("messages = %d", len(loaded.Messages))
	}
	if loaded.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should not be zero")
	}
}

func TestInMemoryAskSessionStoreSaveEmptyID(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	if err := store.Save(context.Background(), AskSession{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInMemoryAskSessionStoreDelete(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	_ = store.Save(context.Background(), AskSession{ID: "s1"})

	ok, err := store.Delete(context.Background(), "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true for existing session")
	}

	_, found, _ := store.Load(context.Background(), "s1")
	if found {
		t.Fatal("session should have been deleted")
	}
}

func TestInMemoryAskSessionStoreDeleteNotExists(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	ok, err := store.Delete(context.Background(), "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false for nonexistent session")
	}
}

func TestInMemoryAskSessionStoreDeleteEmptyID(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	ok, err := store.Delete(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false for empty ID")
	}
}

func TestInMemoryAskSessionStoreContextCancelled(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	_ = store.Save(context.Background(), AskSession{ID: "s1"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := store.Load(ctx, "s1")
	if err == nil {
		t.Fatal("expected context cancelled error from Load")
	}
	err = store.Save(ctx, AskSession{ID: "s1"})
	if err == nil {
		t.Fatal("expected context cancelled error from Save")
	}
	_, err = store.Delete(ctx, "s1")
	if err == nil {
		t.Fatal("expected context cancelled error from Delete")
	}
}

func TestAskSessionStoreCleanupExpired(t *testing.T) {
	store := newInMemoryAskSessionStore(10 * time.Millisecond)
	_ = store.Save(context.Background(), AskSession{ID: "s1"})
	_ = store.Save(context.Background(), AskSession{ID: "s2"})

	time.Sleep(20 * time.Millisecond)

	_, found, _ := store.Load(context.Background(), "s1")
	if found {
		t.Fatal("s1 should have expired")
	}
	_, found, _ = store.Load(context.Background(), "s2")
	if found {
		t.Fatal("s2 should have expired")
	}
}

func TestAskSessionStoreDefaultTTL(t *testing.T) {
	store := newInMemoryAskSessionStore(0)
	impl := store.(*inMemoryAskSessionStore)
	if impl.ttl != askSessionTTL {
		t.Fatalf("ttl = %v, want %v", impl.ttl, askSessionTTL)
	}
}

func TestAskSessionStoreUpdateRefreshesTimestamp(t *testing.T) {
	store := newInMemoryAskSessionStore(100 * time.Millisecond)
	_ = store.Save(context.Background(), AskSession{ID: "s1"})

	time.Sleep(50 * time.Millisecond)
	_, found, _ := store.Load(context.Background(), "s1")
	if !found {
		t.Fatal("s1 should still be alive after Load refresh")
	}

	time.Sleep(60 * time.Millisecond)
	_, found, _ = store.Load(context.Background(), "s1")
	if !found {
		t.Fatal("s1 should still be alive due to timestamp refresh")
	}
}

func TestAskSessionStoreCleanupHandlesZeroUpdateTime(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	impl := store.(*inMemoryAskSessionStore)
	impl.sessions["s1"] = AskSession{ID: "s1"}
	impl.cleanupExpiredLocked(time.Now().UTC())
	session, exists := impl.sessions["s1"]
	if !exists {
		t.Fatal("session with zero updatedAt should survive cleanup")
	}
	if session.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be patched to now")
	}
}

func TestAskSessionStoreNilCleanup(t *testing.T) {
	var s *inMemoryAskSessionStore
	s.cleanupExpiredLocked(time.Now())
}

func TestAskSessionStoreNilCleanupZeroTTL(t *testing.T) {
	s := &inMemoryAskSessionStore{ttl: 0}
	s.cleanupExpiredLocked(time.Now())
}

func TestAskSessionStoreSaveWithoutCreatedAt(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	session := AskSession{
		ID:      "s1",
		Workdir: "/tmp",
	}
	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	loaded, found, _ := store.Load(context.Background(), "s1")
	if !found {
		t.Fatal("expected session to be found")
	}
	if loaded.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be auto-filled")
	}
}

func TestAskSessionStoreSaveConflictIsLastWriteWins(t *testing.T) {
	store := newInMemoryAskSessionStore(time.Hour)
	_ = store.Save(context.Background(), AskSession{ID: "s1", Workdir: "/first"})
	_ = store.Save(context.Background(), AskSession{ID: "s1", Workdir: "/second"})

	loaded, found, _ := store.Load(context.Background(), "s1")
	if !found {
		t.Fatal("expected session to be found")
	}
	if loaded.Workdir != "/second" {
		t.Fatalf("Workdir = %q, want /second", loaded.Workdir)
	}
}
