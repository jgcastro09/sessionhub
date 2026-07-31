// Package download is the shared "fetch a file over HTTP, cap its size, and
// verify it against a hardcoded SHA-256 before trusting it" primitive every
// self-installed local tool uses: internal/voice's Whisper server/model and
// internal/embedding's llama.cpp server/model both go through here so the
// verification logic exists in exactly one place.
package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
)

// Progress reports an installation step or byte-level download progress.
// Total is zero when the remote server does not provide a content length.
type Progress struct {
	Stage   string
	Current int64
	Total   int64
}

// ProgressReporter is optional so callers outside a UI can retain a simple
// synchronous installation API.
type ProgressReporter func(Progress)

// Report calls report if it is non-nil.
func Report(report ProgressReporter, progress Progress) {
	if report != nil {
		report(progress)
	}
}

// Verified GETs url, enforces a byte cap, and checks the result against
// expectedSHA256 before returning it. Every self-installed binary or model
// in SessionHub goes through this exact function — never a bespoke
// downloader — so there is exactly one place that decides a download is
// trustworthy.
func Verified(ctx context.Context, url, expectedSHA256 string, maximum int64, stage string, report ProgressReporter) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "sessionhub-installer")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: server returned %s", url, response.Status)
	}
	if response.ContentLength > maximum {
		return nil, fmt.Errorf("download %s exceeded %d bytes", url, maximum)
	}
	total := response.ContentLength
	Report(report, Progress{Stage: stage, Total: total})
	var data bytes.Buffer
	buffer := make([]byte, 64<<10)
	var downloaded int64
	lastPercent := int64(-1)
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			downloaded += int64(read)
			if downloaded > maximum {
				return nil, fmt.Errorf("download %s exceeded %d bytes", url, maximum)
			}
			if _, err := data.Write(buffer[:read]); err != nil {
				return nil, err
			}
			if total <= 0 || downloaded*100/total != lastPercent {
				if total > 0 {
					lastPercent = downloaded * 100 / total
				}
				Report(report, Progress{Stage: stage, Current: downloaded, Total: total})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	Report(report, Progress{Stage: "Verifying " + stage, Current: downloaded, Total: total})
	payload := data.Bytes()
	sum := sha256.Sum256(payload)
	actual := hex.EncodeToString(sum[:])
	if actual != expectedSHA256 {
		return nil, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, expectedSHA256, actual)
	}
	return payload, nil
}
