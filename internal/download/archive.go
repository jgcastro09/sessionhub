package download

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractTarGz flattens every regular file and symlink in the archive
// directly into destDir, discarding any directory structure (a single
// top-level "toolname-version/" component, or none at all). Symlinks matter:
// prebuilt macOS/Linux shared-library archives (whisper.cpp, llama.cpp) ship
// a real, fully-versioned file (libfoo.1.2.3.dylib/.so) plus shorter
// compat-version symlinks (libfoo.1.dylib/.so) that executables actually
// reference via @rpath/$ORIGIN — skipping symlink entries breaks the load at
// runtime ("Library not loaded").
func ExtractTarGz(archive []byte, destDir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		targetPath := filepath.Join(destDir, filepath.Base(header.Name))
		switch header.Typeflag {
		case tar.TypeReg:
			if err := extractTarFile(tr, targetPath); err != nil {
				return fmt.Errorf("extract %s: %w", header.Name, err)
			}
		case tar.TypeSymlink:
			_ = os.Remove(targetPath)
			if err := os.Symlink(filepath.Base(header.Linkname), targetPath); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", header.Name, header.Linkname, err)
			}
		}
	}
}

func extractTarFile(tr *tar.Reader, targetPath string) error {
	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, tr)
	return err
}

// ExtractZip flattens the archive's (optional) single top-level folder
// directly into destDir, the same way ExtractTarGz does for tar.gz.
func ExtractZip(archive []byte, destDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		name := file.Name
		if idx := strings.IndexByte(name, '/'); idx >= 0 {
			name = name[idx+1:]
		}
		if name == "" || file.FileInfo().IsDir() {
			continue
		}
		targetPath := filepath.Join(destDir, filepath.Base(name))
		if err := extractZipFile(file, targetPath); err != nil {
			return fmt.Errorf("extract %s: %w", file.Name, err)
		}
	}
	return nil
}

func extractZipFile(file *zip.File, targetPath string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}
