package session

import (
	"context"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

func TestSessionSkillActivationHelpers(t *testing.T) {
	session := New("skills")
	if !session.ActivateSkill("  Go_Review  ") {
		t.Fatalf("expected first activation to report change")
	}
	if session.ActivateSkill("go-review") {
		t.Fatalf("expected duplicate activation to be idempotent")
	}
	if !session.ActivateSkill("zeta") {
		t.Fatalf("expected second activation to report change")
	}
	if got := session.ActiveSkillIDs(); len(got) != 2 || got[0] != "go-review" || got[1] != "zeta" {
		t.Fatalf("unexpected active skill ids: %+v", got)
	}
	if !session.DeactivateSkill("GO_REVIEW") {
		t.Fatalf("expected deactivate to remove normalized id")
	}
	if session.DeactivateSkill("go-review") {
		t.Fatalf("expected duplicate deactivate to be idempotent")
	}
	if got := session.ActiveSkillIDs(); len(got) != 1 || got[0] != "zeta" {
		t.Fatalf("unexpected active skill ids after deactivate: %+v", got)
	}
}

func TestSQLiteStoreRoundTripActivatedSkills(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{
		ID:        "skills_round_trip",
		Title:     "Skills Round Trip",
		CreatedAt: time.Now().Add(-time.Minute),
		UpdatedAt: time.Now(),
		ActivatedSkills: []SkillActivation{
			{SkillID: "  zeta "},
			{SkillID: "go_review"},
			{SkillID: "go-review"},
		},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got := session.ActiveSkillIDs(); len(got) != 2 || got[0] != "go-review" || got[1] != "zeta" {
		t.Fatalf("expected normalized in-memory activations, got %+v", got)
	}

	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages: []providertypes.Message{
			{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
		},
	}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	loaded, err := store.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if got := loaded.ActiveSkillIDs(); len(got) != 2 || got[0] != "go-review" || got[1] != "zeta" {
		t.Fatalf("expected normalized loaded activations, got %+v", got)
	}
}

func TestSkillActivationHelpersHandleNilSessionAndBlankInput(t *testing.T) {
	var nilSession *Session
	if nilSession.ActivateSkill("go-review") {
		t.Fatalf("expected nil session activate to be no-op")
	}
	if nilSession.DeactivateSkill("go-review") {
		t.Fatalf("expected nil session deactivate to be no-op")
	}

	session := New("blank")
	if session.ActivateSkill("   ") {
		t.Fatalf("expected blank skill id to be rejected")
	}
	if session.DeactivateSkill("   ") {
		t.Fatalf("expected blank deactivation to be rejected")
	}
}

func TestSkillActivationCloneHelpers(t *testing.T) {
	original := []SkillActivation{{SkillID: "go-review"}, {SkillID: "zeta"}}
	cloned := cloneSkillActivations(original)
	if len(cloned) != len(original) {
		t.Fatalf("expected clone length %d, got %d", len(original), len(cloned))
	}

	cloned[0].SkillID = "changed"
	if original[0].SkillID != "go-review" {
		t.Fatalf("expected source not to be mutated, got %+v", original)
	}

	if got := (SkillActivation{SkillID: "go-review"}).Clone(); got.SkillID != "go-review" {
		t.Fatalf("unexpected clone result: %+v", got)
	}

	if cloneSkillActivations(nil) != nil {
		t.Fatalf("expected nil input to cloneSkillActivations to return nil")
	}
}
