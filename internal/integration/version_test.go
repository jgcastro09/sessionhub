package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseVersionsStayInSync(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	versionData, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionData))
	var packageJSON struct {
		Version string `json:"version"`
	}
	data, err := os.ReadFile(filepath.Join(root, "npm", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &packageJSON); err != nil {
		t.Fatal(err)
	}
	if packageJSON.Version != version {
		t.Fatalf("VERSION=%s but npm package=%s", version, packageJSON.Version)
	}
}
