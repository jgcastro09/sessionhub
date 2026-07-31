//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework AppKit

#import <Cocoa/Cocoa.h>

static void setMacDockIconNative(const char* imagePath) {
    @autoreleasepool {
        NSApplication* app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyRegular];
        
        NSString* path = [NSString stringWithUTF8String:imagePath];
        NSImage* image = [[NSImage alloc] initWithContentsOfFile:path];
        if (image != nil) {
            [app setApplicationIconImage:image];
            [app activateIgnoringOtherApps:YES];
        }
    }
}
*/
import "C"
import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"

	"github.com/jgcastro09/sessionhub/internal/assets"
)

func setOSDockIcon(logoPath string) {
	if logoPath == "" {
		return
	}
	root := filepath.Dir(logoPath)
	_, icnsPath, _ := assets.EnsureLogoExtracted(root)

	ensureMacAppBundle(root, icnsPath)

	cPath := C.CString(logoPath)
	defer C.free(unsafe.Pointer(cPath))
	C.setMacDockIconNative(cPath)
}

func ensureMacAppBundle(root, icnsPath string) {
	if root == "" {
		return
	}
	appDir := filepath.Join(root, "SessionHub.app")
	contentsDir := filepath.Join(appDir, "Contents")
	macOSDir := filepath.Join(contentsDir, "MacOS")
	resourcesDir := filepath.Join(contentsDir, "Resources")

	_ = os.MkdirAll(macOSDir, 0o755)
	_ = os.MkdirAll(resourcesDir, 0o755)

	plistContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>SessionHub</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIdentifier</key>
    <string>com.jgcastro09.sessionhub</string>
    <key>CFBundleName</key>
    <string>Session Hub</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
</dict>
</plist>`

	_ = os.WriteFile(filepath.Join(contentsDir, "Info.plist"), []byte(plistContent), 0o644)

	if icnsData, err := os.ReadFile(icnsPath); err == nil {
		_ = os.WriteFile(filepath.Join(resourcesDir, "AppIcon.icns"), icnsData, 0o644)
	}

	launcherScript := fmt.Sprintf("#!/bin/bash\nexec sessionhub \"$@\"\n")
	_ = os.WriteFile(filepath.Join(macOSDir, "SessionHub"), []byte(launcherScript), 0o755)

	lsregister := "/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister"
	if _, err := os.Stat(lsregister); err == nil {
		_ = exec.Command(lsregister, "-f", appDir).Run()
	}
}
