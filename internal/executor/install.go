package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// InstallDirs is the on-disk layout for one executor, fully self-contained
// under SessionHub's own data folder:
//
//	executors/<slug>/bin/       the CLI's executable(s)
//	executors/<slug>/config/    reserved for the CLI's own config, if wired up
//	executors/<slug>/runtime/   reserved for scratch/runtime state
//	executors/<slug>/manifest.json
type InstallDirs struct {
	Root    string
	Bin     string
	Config  string
	Runtime string
}

var slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a display name into a stable folder name (executors/<slug>).
// It's derived once at install time; renaming the executor afterward does
// not move its folder.
func Slug(name string) string {
	slug := strings.Trim(slugInvalid.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		return "cli"
	}
	return slug
}

// EnsureInstallDirs creates (if needed) and returns the managed folder for
// one executor under executorsRoot.
func EnsureInstallDirs(executorsRoot, slug string) (InstallDirs, error) {
	root := filepath.Join(executorsRoot, slug)
	dirs := InstallDirs{
		Root:    root,
		Bin:     filepath.Join(root, "bin"),
		Config:  filepath.Join(root, "config"),
		Runtime: filepath.Join(root, "runtime"),
	}
	for _, dir := range []string{dirs.Bin, dirs.Config, dirs.Runtime} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return InstallDirs{}, err
		}
	}
	return dirs, nil
}

// Manifest records how an executor came to be installed, written alongside
// its bin/config/runtime folders for inspection.
type Manifest struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Command        string    `json:"command"`
	BinaryName     string    `json:"binary_name,omitempty"`
	InstallCmd     string    `json:"install_command,omitempty"`
	InstalledAt    time.Time `json:"installed_at"`
	AlreadyPresent bool      `json:"already_present"`
}

// WriteManifest persists manifest.json in the executor's own root folder.
func WriteManifest(dirs InstallDirs, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dirs.Root, "manifest.json"), data, 0o600)
}

// ResetAccount wipes an executor's isolated config/ and runtime/ folders —
// its login/session state — leaving bin/ and manifest.json untouched. A
// hard reset of the account without needing to reinstall the CLI.
func ResetAccount(installDir string) error {
	for _, sub := range []string{"config", "runtime"} {
		dir := filepath.Join(installDir, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, entry := range entries {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReadManifest loads manifest.json from an executor's own root folder, if
// present — used to recover the original install command for updates.
func ReadManifest(installDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(installDir, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ScanInstalled walks executorsRoot for CLIs that were installed before —
// each a folder holding a manifest.json — and returns the ones whose
// recorded binary still exists on disk. This lets a fresh or wiped
// database recover already-installed CLIs without reinstalling anything.
func ScanInstalled(executorsRoot string) ([]Manifest, error) {
	entries, err := os.ReadDir(executorsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var manifests []Manifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(executorsRoot, entry.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil || manifest.Command == "" {
			continue
		}
		if info, statErr := os.Stat(manifest.Command); statErr != nil || info.IsDir() {
			continue // stale: the binary this manifest points to is gone
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}
