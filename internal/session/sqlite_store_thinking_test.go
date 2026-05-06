package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	providertypes "neo-code/internal/provider/types"
)

func TestSQLiteStoreRoundTripsThinkingMetadata(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "thinking_meta", Title: "thinking"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	thinking := json.RawMessage(`{"reasoning_content":"plan"}`)
	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages: []providertypes.Message{{
			Role:             providertypes.RoleAssistant,
			Parts:            []providertypes.ContentPart{providertypes.NewTextPart("answer")},
			ThinkingMetadata: thinking,
		}},
	}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	loaded, err := store.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("expected one message, got %d", len(loaded.Messages))
	}
	if string(loaded.Messages[0].ThinkingMetadata) != string(thinking) {
		t.Fatalf("thinking metadata = %s, want %s", loaded.Messages[0].ThinkingMetadata, thinking)
	}
}

func TestMigrateSQLiteSchemaV6ToV7AddsThinkingMetadataColumn(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "schema-v6.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE messages (
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			parts_json TEXT NOT NULL,
			tool_calls_json TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			is_error INTEGER NOT NULL DEFAULT 0,
			tool_metadata_json TEXT NOT NULL DEFAULT '',
			created_at_ms INTEGER NOT NULL,
			PRIMARY KEY(session_id, seq)
		)`,
		`PRAGMA user_version=6`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("Exec(%q) error = %v", statement, err)
		}
	}

	if err := migrateSQLiteSchemaV6ToV7(context.Background(), db); err != nil {
		t.Fatalf("migrateSQLiteSchemaV6ToV7() error = %v", err)
	}
	hasColumn, err := sqliteTableHasColumn(context.Background(), mustBeginTx(t, db), "messages", "thinking_metadata_json")
	if err != nil {
		t.Fatalf("sqliteTableHasColumn() error = %v", err)
	}
	if !hasColumn {
		t.Fatal("expected thinking_metadata_json column after migration")
	}
}

func mustBeginTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback()
	})
	return tx
}
