package executor

import (
	"runtime"
	"testing"
)

func TestCatalogForOSFiltering(t *testing.T) {
	catalog := CatalogForOS()
	if len(catalog) == 0 {
		t.Fatalf("expected non-empty catalog for OS %s", runtime.GOOS)
	}

	for _, p := range catalog {
		if len(p.SupportedOS) > 0 {
			matched := false
			for _, osName := range p.SupportedOS {
				if osName == runtime.GOOS {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("provider %q with SupportedOS %v should not be included on %s", p.Name, p.SupportedOS, runtime.GOOS)
			}
		}
	}

	if runtime.GOOS == "darwin" {
		for _, p := range catalog {
			if p.Name == "PowerShell" || p.Name == "CMD (Command Prompt)" || p.Name == "WSL (Linux)" {
				t.Errorf("windows terminal %q should be excluded on darwin", p.Name)
			}
		}
	} else if runtime.GOOS == "windows" {
		for _, p := range catalog {
			if p.Name == "Zsh (Terminal)" {
				t.Errorf("macOS/unix terminal %q should be excluded on windows", p.Name)
			}
		}
	}
}
