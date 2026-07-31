//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework AppKit

#import <Cocoa/Cocoa.h>

static void setMacDockIconNative(const char* imagePath) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        NSString* path = [NSString stringWithUTF8String:imagePath];
        NSImage* image = [[NSImage alloc] initWithContentsOfFile:path];
        if (image != nil) {
            [NSApp setApplicationIconImage:image];
        }
    }
}
*/
import "C"
import (
	"unsafe"
)

func setOSDockIcon(logoPath string) {
	if logoPath == "" {
		return
	}
	cPath := C.CString(logoPath)
	defer C.free(unsafe.Pointer(cPath))
	C.setMacDockIconNative(cPath)
}
