package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Body    string  `json:"body"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Checker struct {
	Owner      string
	Repository string
	Client     *http.Client
}

func NewChecker(owner, repository string) *Checker {
	return &Checker{
		Owner: owner, Repository: repository,
		Client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Checker) Latest(ctx context.Context) (Release, error) {
	if c.Owner == "" || c.Repository == "" {
		return Release{}, fmt.Errorf("update repository owner and name are required")
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", c.Owner, c.Repository)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "sessionhub-update-checker")
	response, err := c.Client.Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("check official release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("check official release: GitHub returned %s", response.Status)
	}
	var release Release
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("decode official release: %w", err)
	}
	return release, nil
}

func IsNewer(current, candidate string) bool {
	currentParts := semanticParts(current)
	candidateParts := semanticParts(candidate)
	for i := 0; i < 3; i++ {
		if candidateParts[i] != currentParts[i] {
			return candidateParts[i] > currentParts[i]
		}
	}
	return false
}

func semanticParts(value string) [3]int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		out[i], _ = strconv.Atoi(parts[i])
	}
	return out
}

func AssetName(version string) string {
	version = strings.TrimPrefix(version, "v")
	return fmt.Sprintf("sessionhub_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
}

// DownloadVerified downloads the matching release archive and validates its
// hash against checksums.txt before returning the staging path.
func (c *Checker) DownloadVerified(
	ctx context.Context,
	release Release,
	destination string,
) (string, error) {
	assetName := AssetName(release.TagName)
	var assetURL, checksumURL string
	for _, asset := range release.Assets {
		switch asset.Name {
		case assetName:
			assetURL = asset.BrowserDownloadURL
		case "checksums.txt":
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if assetURL == "" || checksumURL == "" {
		return "", fmt.Errorf("release is missing %s or checksums.txt", assetName)
	}
	checksums, err := c.download(ctx, checksumURL, 2<<20)
	if err != nil {
		return "", err
	}
	expected, err := checksumFor(checksums, assetName)
	if err != nil {
		return "", err
	}
	archive, err := c.download(ctx, assetURL, 256<<20)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(archive)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, expected, actual)
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(destination, assetName)
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		return "", fmt.Errorf("write verified update: %w", err)
	}
	return path, nil
}

func (c *Checker) download(ctx context.Context, url string, maximum int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "sessionhub-update-checker")
	response, err := c.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download release asset: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download release asset: server returned %s", response.Status)
	}
	reader := io.LimitReader(response.Body, maximum+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("release asset exceeds %d bytes", maximum)
	}
	return data, nil
}

func checksumFor(data []byte, name string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			if len(fields[0]) != 64 {
				return "", fmt.Errorf("invalid SHA-256 checksum for %s", name)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// ApplySelfUpdate downloads, verifies, extracts, and replaces the running executable.
func (c *Checker) ApplySelfUpdate(ctx context.Context, release Release) error {
	tmpDir, err := os.MkdirTemp("", "sessionhub-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath, err := c.DownloadVerified(ctx, release, tmpDir)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}

	extractedBinary, err := extractBinaryFromTarGz(archivePath, tmpDir)
	if err != nil {
		return fmt.Errorf("extract update binary: %w", err)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}

	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("eval symlinks: %w", err)
	}

	return replaceExecutable(execPath, extractedBinary)
}

func extractBinaryFromTarGz(archivePath, destDir string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	expectedName := "sessionhub"
	if runtime.GOOS == "windows" {
		expectedName = "sessionhub.exe"
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		baseName := filepath.Base(header.Name)
		if baseName == expectedName || baseName == "sessionhub" || baseName == "sessionhub.exe" {
			targetPath := filepath.Join(destDir, expectedName)
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
			return targetPath, nil
		}
	}
	return "", fmt.Errorf("binary %s not found in update archive", expectedName)
}

func replaceExecutable(currentPath, newBinaryPath string) error {
	if runtime.GOOS == "windows" {
		oldPath := currentPath + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(currentPath, oldPath); err != nil {
			return fmt.Errorf("rename current executable to .old: %w", err)
		}
		if err := copyFileContents(newBinaryPath, currentPath); err != nil {
			_ = os.Rename(oldPath, currentPath)
			return fmt.Errorf("copy new binary: %w", err)
		}
		return nil
	}

	tempPath := currentPath + ".new"
	if err := copyFileContents(newBinaryPath, tempPath); err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}
	_ = os.Chmod(tempPath, 0o755)
	return os.Rename(tempPath, currentPath)
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
