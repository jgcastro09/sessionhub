// Package safego provides a single, shared panic-recovery wrapper for
// goroutine bodies.
//
// SessionHub's TUI puts the user's real terminal into raw mode, alt-screen,
// mouse-tracking, and Kitty keyboard mode while it runs (via Bubble Tea).
// Bubble Tea restores all of that on a clean exit, Ctrl+C, or SIGTERM — but
// only through a defer in its own Run() goroutine. An uncaught panic in any
// other goroutine terminates the whole Go process immediately and skips
// that restore entirely, stranding the user's real terminal in a broken
// input mode (keystrokes render as different characters, terminal appears
// frozen) that survives even after the process has exited. See AGENTS.md's
// Terminal-Safety Guarantee.
//
// Run must be called synchronously as the first thing inside a goroutine
// you have already started with `go`; it does not spawn one itself, so
// call-site defers (waitgroups, semaphores) keep firing in the correct
// order during a panic unwind.
package safego

import "log"

// Run executes fn, recovering any panic instead of letting it escape and
// crash the process. name identifies the goroutine in the recovered log
// line for debugging.
func Run(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("sessionhub: recovered panic in %s: %v", name, r)
		}
	}()
	fn()
}
