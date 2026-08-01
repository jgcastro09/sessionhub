package registry

import "testing"

// TestConfigValidateRejectsInvalidRoots is the regression test for the
// Fase 0 gap where Config.Validate() never checked Roots at all — an
// absolute root, a root escaping the project via "..", or two overlapping
// roots could all reach config.json unrejected via SaveConfig.
func TestConfigValidateRejectsInvalidRoots(t *testing.T) {
	cases := []struct {
		name  string
		roots []string
	}{
		{"absolute", []string{"/etc"}},
		{"parent-escape", []string{"../outside"}},
		{"parent-escape-nested", []string{"src/../../outside"}},
		{"empty-entry", []string{""}},
		{"self-overlap", []string{"src", "src"}},
		{"nested-overlap", []string{"src", "src/lib"}},
		{"nested-overlap-reversed", []string{"src/lib", "src"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.Roots = c.roots
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected roots %v to be rejected", c.roots)
			}
		})
	}
}

func TestConfigValidateAcceptsDisjointRelativeRoots(t *testing.T) {
	cases := [][]string{
		{"."},
		{"src", "docs"},
		{"cmd", "internal", "web/src"},
	}
	for _, roots := range cases {
		cfg := defaultConfig()
		cfg.Roots = roots
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected roots %v to be accepted, got %v", roots, err)
		}
	}
}

// TestSaveConfigRejectsInvalidRoot exercises the same rejection through the
// Service, the path a Web Panel or CLI edit actually takes.
func TestSaveConfigRejectsInvalidRoot(t *testing.T) {
	svc, projectID, _ := newTestService(t)
	cfg, err := svc.LoadConfig(projectID)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Roots = []string{"../outside"}
	if err := svc.SaveConfig(projectID, cfg); err == nil {
		t.Fatalf("expected SaveConfig to reject an escaping root")
	}
}
