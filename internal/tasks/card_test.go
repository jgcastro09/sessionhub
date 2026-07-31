package tasks

import (
	"testing"
	"time"
)

func TestCardRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	card := NewCard("TASK-0001", "Add tests", "feature", PriorityHigh, now)
	card.ImpactedAreas = []string{"internal/tasks", "web/src"}
	card.RegistryRefs = []string{"entry:abc123"}
	card.setSection("Resumo", "Resumo curto da tarefa.")
	card.setSection("Critérios de aceite", "- Testes passam\n- Sem regressão")

	data := card.Marshal()
	parsed, err := ParseCard(data)
	if err != nil {
		t.Fatalf("ParseCard: %v", err)
	}

	again := parsed.Marshal()
	if string(data) != string(again) {
		t.Fatalf("round trip is not stable:\n--- first ---\n%s\n--- second ---\n%s", data, again)
	}
	if parsed.Title != card.Title || parsed.Status != card.Status {
		t.Fatalf("parsed card lost fields: %+v", parsed)
	}
	if len(parsed.ImpactedAreas) != 2 || parsed.ImpactedAreas[1] != "web/src" {
		t.Fatalf("impacted areas not preserved: %v", parsed.ImpactedAreas)
	}
	if parsed.section("Resumo") != "Resumo curto da tarefa." {
		t.Fatalf("section content not preserved: %q", parsed.section("Resumo"))
	}
}

func TestCardMarshalIsIdempotentAfterUnrelatedFieldChange(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	card := NewCard("TASK-0002", "Fix bug", "bug", PriorityMedium, now)
	card.setSection("Resumo", "Descrição do bug.")
	before := card.Marshal()

	parsed, err := ParseCard(before)
	if err != nil {
		t.Fatalf("ParseCard: %v", err)
	}
	parsed.Priority = PriorityUrgent
	after := parsed.Marshal()

	if string(before) == string(after) {
		t.Fatalf("expected priority change to alter output")
	}
	reparsed, err := ParseCard(after)
	if err != nil {
		t.Fatalf("ParseCard after edit: %v", err)
	}
	if string(after) != string(reparsed.Marshal()) {
		t.Fatalf("edited card is not stable across a second round trip")
	}
	if reparsed.section("Resumo") != "Descrição do bug." {
		t.Fatalf("unrelated section changed: %q", reparsed.section("Resumo"))
	}
}

func TestParseCardRejectsMissingFrontmatter(t *testing.T) {
	if _, err := ParseCard([]byte("no frontmatter here")); err == nil {
		t.Fatal("expected an error for a card without frontmatter")
	}
}
