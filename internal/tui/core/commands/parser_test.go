package commands

import "testing"

func TestMatchSlashCommands(t *testing.T) {
	commands := []SlashCommand{
		{Usage: "/help", Description: "show help"},
		{Usage: "/provider", Description: "pick provider"},
		{Usage: "/provider add", Description: "add provider"},
		{Usage: "/model", Description: "pick model"},
	}

	got := MatchSlashCommands("/pro", "/", commands)
	if len(got) == 0 {
		t.Fatalf("expected suggestions for /pro, got %d", len(got))
	}
	if got[0].Command.Usage != "/provider" || !got[0].Match {
		t.Fatalf("unexpected suggestion: %+v", got[0])
	}

	if complete := MatchSlashCommands("/help", "/", commands); complete != nil {
		t.Fatalf("expected nil suggestion when command is complete, got %+v", complete)
	}

	fuzzy := MatchSlashCommands("/mdl", "/", commands)
	if len(fuzzy) != 1 {
		t.Fatalf("expected one fuzzy suggestion for /mdl, got %d", len(fuzzy))
	}
	if fuzzy[0].Command.Usage != "/model" {
		t.Fatalf("expected /model for fuzzy query /mdl, got %+v", fuzzy[0])
	}

	multiWord := MatchSlashCommands("/provider ", "/", commands)
	if len(multiWord) == 0 {
		t.Fatalf("expected multi-word slash suggestions after trailing space")
	}
	if multiWord[0].Command.Usage != "/provider add" {
		t.Fatalf("expected /provider add suggestion, got %+v", multiWord[0])
	}
}

func TestIsCompleteSlashCommand(t *testing.T) {
	commands := []SlashCommand{{Usage: "/help"}, {Usage: "/provider"}}
	if !IsCompleteSlashCommand("/help", commands) {
		t.Fatalf("expected /help to be complete")
	}
	if IsCompleteSlashCommand("/hel", commands) {
		t.Fatalf("expected /hel to be incomplete")
	}
	if IsCompleteSlashCommand("/provider ", commands) {
		t.Fatalf("expected trailing-space input to remain incomplete")
	}
}

func TestSplitFirstWord(t *testing.T) {
	first, rest := SplitFirstWord(" /cwd   ./tmp/project ")
	if first != "/cwd" || rest != "./tmp/project" {
		t.Fatalf("unexpected split result: first=%q rest=%q", first, rest)
	}

	first, rest = SplitFirstWord("   ")
	if first != "" || rest != "" {
		t.Fatalf("expected empty split for blank input, got first=%q rest=%q", first, rest)
	}
}
