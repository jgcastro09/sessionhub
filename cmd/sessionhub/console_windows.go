//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableConsoleMouseInput clears Quick Edit Mode and enables mouse input on
// the console's stdin handle. Classic Windows PowerShell (conhost.exe) has
// Quick Edit Mode on by default, which intercepts every mouse click for
// text selection and never forwards it to the process; bubbletea's own raw
// mode setup (charmbracelet/x/term) enables VT input but never touches this
// bit, so without this, clicks on the tab bar/sidebar silently do nothing.
// Changing ENABLE_QUICK_EDIT_MODE only takes effect together with
// ENABLE_EXTENDED_FLAGS in the same call, per the Windows console API.
// Windows Terminal isn't affected by this.
func enableConsoleMouseInput() {
	handle := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return
	}
	mode &^= windows.ENABLE_QUICK_EDIT_MODE
	mode |= windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_MOUSE_INPUT
	_ = windows.SetConsoleMode(handle, mode)
}
