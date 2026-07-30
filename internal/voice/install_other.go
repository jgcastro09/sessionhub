//go:build !windows && !darwin

package voice

import "context"

// EnsureInstalled is a no-op stand-in on platforms without a whisper.cpp
// acquisition path yet (only Windows and macOS are implemented so far),
// keeping the package importable everywhere the recorder stub also does.
func EnsureInstalled(ctx context.Context, toolsRoot string, report ProgressReporter) (Installed, error) {
	return Installed{}, ErrUnsupportedPlatform
}
