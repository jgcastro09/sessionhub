package registry

import "testing"

func TestClassificationPolicyValidateRequiresFallback(t *testing.T) {
	p := ClassificationPolicy{
		CategoryRules: []CategoryRule{{Extensions: []string{".go"}, Category: "go"}},
		KindRules:     []KindRule{{Fallback: true, Kind: KindOther}},
	}
	if err := p.Validate(); err == nil {
		t.Fatalf("expected Validate to require a trailing fallback category rule")
	}
}

func TestClassificationOrderedRulesFirstMatchWins(t *testing.T) {
	p := ClassificationPolicy{
		CategoryRules: []CategoryRule{
			{PathPrefixes: []string{"scripts/"}, Category: "build-scripts"},
			{Extensions: []string{".py"}, Category: "python-tools"},
			{Fallback: true, Category: "uncategorized"},
		},
		KindRules: []KindRule{
			{Fallback: true, Kind: KindOther},
		},
	}
	// scripts/deploy.py matches both rules — the earlier rule (path prefix)
	// must win since rules are evaluated in order.
	category, _ := p.Classify("scripts/deploy.py")
	if category != "build-scripts" {
		t.Fatalf("expected the earlier path-prefix rule to win, got %q", category)
	}
	category, _ = p.Classify("tools/build.py")
	if category != "python-tools" {
		t.Fatalf("expected the extension rule to match outside scripts/, got %q", category)
	}
	category, _ = p.Classify("README.md")
	if category != "uncategorized" {
		t.Fatalf("expected the fallback rule for an unmatched extension, got %q", category)
	}
}

func TestDefaultClassificationPolicyIsTotal(t *testing.T) {
	p := defaultClassificationPolicy()
	if err := p.Validate(); err != nil {
		t.Fatalf("default policy must validate: %v", err)
	}
	for _, path := range []string{"main.go", "a/b/c.rs", "README.md", "weird.unknownext"} {
		category, kind := p.Classify(path)
		if category == "" || kind == "" {
			t.Fatalf("Classify(%q) must always return a non-empty category/kind, got %q/%q", path, category, kind)
		}
	}
}

func TestKindRuleTestGlobBeatsGenericImplementation(t *testing.T) {
	p := defaultClassificationPolicy()
	_, kind := p.Classify("internal/registry/service_test.go")
	if kind != KindTest {
		t.Fatalf("expected _test.go to classify as KindTest, got %q", kind)
	}
	_, kind = p.Classify("internal/registry/service.go")
	if kind != KindImplementation {
		t.Fatalf("expected a plain .go file to classify as KindImplementation, got %q", kind)
	}
}
