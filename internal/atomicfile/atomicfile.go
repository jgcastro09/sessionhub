// Package atomicfile writes files the way every .shproject-owning service
// must: to a temp file in the same directory, then rename into place, so a
// crash or concurrent read never observes a partial write.
package atomicfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ErrExists is returned by CreateExclusive when path already exists.
var ErrExists = errors.New("file already exists")

// Write atomically replaces path with data. The temp file lives beside path
// so the final os.Rename stays on the same filesystem/volume.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sessionhub-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// CreateExclusive writes data to path only if path does not already exist.
// Callers that mint sequential IDs (task cards, registry entries) use this
// as the final collision guard even after computing "the next free ID"
// under an in-process lock, since a second sessionhub process could race it.
func CreateExclusive(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		if os.IsExist(err) {
			return ErrExists
		}
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

// WriteJSON marshals value as indented JSON with a trailing newline and
// writes it atomically.
func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return Write(path, data, 0o644)
}
