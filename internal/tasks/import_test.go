package tasks

import (
	"strings"
	"testing"
)

const validDraft = `* Título Direto: Implementar Media Manager e Collect Project

* Resumo de Uma Frase: Criar um gerenciador central de arquivos do projeto.

* Tipo: Implementação

* Prioridade: Crítica

* Áreas Envolvidas: Project System, Asset Management, Hot Backup

* Descrição Detalhada:

Manter Asset ID estável e preservar o projeto original.

* Prompt Detalhado para a IA:

# Tarefa

Implementar Media Manager.

## Regras obrigatórias

* Não sobrescrever arquivos diferentes.

* Funcionalidades Esperadas:

* Importação por texto completo.
* Persistência sem campos adicionais.`

func TestParseImportValidDraft(t *testing.T) {
	result := ParseImport(validDraft)
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
	if result.Title != "Implementar Media Manager e Collect Project" {
		t.Fatalf("unexpected title: %q", result.Title)
	}
	if result.Priority != PriorityUrgent {
		t.Fatalf("expected Crítica to canonicalize to urgent, got %q", result.Priority)
	}
	if result.Type != "Implementação" {
		t.Fatalf("unexpected type: %q", result.Type)
	}
	wantAreas := []string{"Project System", "Asset Management", "Hot Backup"}
	if len(result.ImpactedAreas) != len(wantAreas) {
		t.Fatalf("unexpected areas: %v", result.ImpactedAreas)
	}
	for i, area := range wantAreas {
		if result.ImpactedAreas[i] != area {
			t.Fatalf("unexpected areas: %v", result.ImpactedAreas)
		}
	}
	if !strings.Contains(result.AIPrompt, "## Regras obrigatórias") {
		t.Fatalf("ai prompt lost its body: %q", result.AIPrompt)
	}
	if !strings.Contains(result.ExpectedFeatures, "Persistência sem campos adicionais.") {
		t.Fatalf("expected features lost its body: %q", result.ExpectedFeatures)
	}
}

func TestParseImportIncompleteDraft(t *testing.T) {
	result := ParseImport("* Título Direto: Incompleto")
	if result.Valid {
		t.Fatalf("expected invalid draft to fail")
	}
	if len(result.Errors) == 0 {
		t.Fatalf("expected at least one error")
	}
}

func TestParseImportInvalidType(t *testing.T) {
	draft := `* Título Direto: Test
* Resumo de Uma Frase: Test
* Tipo: Feature
* Prioridade: Alta
* Descrição Detalhada: Test
* Prompt Detalhado para a IA: Test
* Funcionalidades Esperadas: Test`
	result := ParseImport(draft)
	if result.Valid {
		t.Fatalf("expected invalid type to fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "Invalid Type") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an Invalid Type error, got: %v", result.Errors)
	}
}

func TestParseImportOversizedField(t *testing.T) {
	draft := `* Título Direto: Test
* Resumo de Uma Frase: Test
* Tipo: Implementação
* Prioridade: Alta
* Descrição Detalhada: Test
* Prompt Detalhado para a IA: ` + strings.Repeat("technical requirement ", 12000) + `
* Funcionalidades Esperadas: Test`
	result := ParseImport(draft)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "9,000-token") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 9,000-token budget error, got: %v", result.Errors)
	}
}

func TestParseImportMarkdownBoldHeadings(t *testing.T) {
	draft := "**Título Direto:** Test\n" +
		"**Resumo de Uma Frase:** Test\n" +
		"**Tipo:** Bug\n" +
		"**Prioridade:** Baixa\n" +
		"**Descrição Detalhada:** Test\n" +
		"**Prompt Detalhado para a IA:** Test\n" +
		"**Funcionalidades Esperadas:** Test"
	result := ParseImport(draft)
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
	if result.Type != "Bug" || result.Priority != PriorityLow {
		t.Fatalf("unexpected type/priority: %q/%q", result.Type, result.Priority)
	}
}

func TestServiceImportCreatesCard(t *testing.T) {
	svc, projectID := newTestService(t)
	card, result, err := svc.Import(projectID, validDraft)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid result, got errors: %v", result.Errors)
	}
	if card.ID == "" {
		t.Fatalf("expected a created card id")
	}
	if card.Title != result.Title {
		t.Fatalf("card title %q does not match parsed title %q", card.Title, result.Title)
	}
	if got := card.section("Prompt sugerido"); !strings.Contains(got, "## Regras obrigatórias") {
		t.Fatalf("expected Prompt sugerido section to carry the AI prompt body, got %q", got)
	}
	if got := card.section("Descrição detalhada"); got == "" {
		t.Fatalf("expected Descrição detalhada section to be populated")
	}
}

func TestServiceImportRejectsInvalidDraftWithoutWriting(t *testing.T) {
	svc, projectID := newTestService(t)
	card, result, err := svc.Import(projectID, "* Título Direto: Incompleto")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected invalid result")
	}
	if card.ID != "" {
		t.Fatalf("expected no card to be created for an invalid draft")
	}
	cards, err := svc.List(projectID, Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected no cards on disk, got %d", len(cards))
	}
}
