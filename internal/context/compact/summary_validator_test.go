package compact

import (
	"strings"
	"testing"
)

func TestCompactSummaryValidatorValidateAcceptsValidSummary(t *testing.T) {
	t.Parallel()

	summary, err := (compactSummaryValidator{}).Validate(validSemanticSummary(), 1200)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !strings.Contains(summary, "done:") {
		t.Fatalf("expected validated summary to preserve sections, got %q", summary)
	}
}

func TestCompactSummaryValidatorValidateRejectsBrokenStructure(t *testing.T) {
	t.Parallel()

	_, err := (compactSummaryValidator{}).Validate("[compact_summary]\ndone:\n- ok", 1200)
	if err == nil {
		t.Fatalf("expected invalid summary error")
	}
}

func TestCompactSummaryValidatorValidateNormalizesWhitespace(t *testing.T) {
	t.Parallel()

	validator := compactSummaryValidator{}
	longSummary := validSemanticSummary() + "\n\n"
	got, err := validator.Validate(longSummary, len([]rune(validSemanticSummary())))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got != validSemanticSummary() {
		t.Fatalf("expected normalized summary, got %q", got)
	}
}

func TestCompactSummaryValidatorValidateShrinksLongSummaryWithinBudget(t *testing.T) {
	t.Parallel()

	lines := []string{"[compact_summary]"}
	for _, section := range summarySections {
		lines = append(lines, "", section+":", "- "+strings.Repeat(section+" detail ", 20), "- extra context")
	}
	summary := strings.Join(lines, "\n")

	got, err := (compactSummaryValidator{}).Validate(summary, 220)
	if err != nil {
		t.Fatalf("expected structured shrink to succeed, got %v", err)
	}
	if len([]rune(got)) > 220 {
		t.Fatalf("expected shrunk summary within budget, got %d runes", len([]rune(got)))
	}
	for _, section := range summarySections {
		if !strings.Contains(got, section+":") {
			t.Fatalf("expected section %q after shrink, got %q", section, got)
		}
	}
}

func TestCompactSummaryValidatorValidateFailsWhenBudgetCannotFitStructure(t *testing.T) {
	t.Parallel()

	_, err := (compactSummaryValidator{}).Validate(validSemanticSummary(), 40)
	if err == nil || !strings.Contains(err.Error(), "max_summary_chars") {
		t.Fatalf("expected impossible budget error, got %v", err)
	}
}
