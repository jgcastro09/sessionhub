//go:build !windows && !darwin && !linux

package embedding

import "context"

// EnsureInstalled is a no-op stand-in on platforms without a known
// llama.cpp release asset. Search stays lexical-only there (plan 5.4).
func EnsureInstalled(ctx context.Context, toolsRoot string, report ProgressReporter) (Installed, error) {
	return Installed{}, ErrUnsupportedPlatform
}
