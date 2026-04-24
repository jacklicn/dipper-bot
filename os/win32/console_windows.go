//go:build windows

package win32

import (
	"syscall"
	"unsafe"
)

const (
	// STD_OUTPUT_HANDLE = -11
	stdOutputHandle           = ^uintptr(10)
	enableProcessedOutput     = 0x0001
	enableVirtualTerminalProc = 0x0004
)

var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleCP    = kernel32.NewProc("SetConsoleCP")
	procSetConsoleOutCP = kernel32.NewProc("SetConsoleOutputCP")
	procGetStdHandle    = kernel32.NewProc("GetStdHandle")
	procGetConsoleMode  = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode  = kernel32.NewProc("SetConsoleMode")
)

// SetConsoleUTF8 sets Windows console to UTF-8 (CP 65001) and enables VT mode for emoji display.
func SetConsoleUTF8() {
	cp := uintptr(65001)
	procSetConsoleCP.Call(cp)
	procSetConsoleOutCP.Call(cp)

	// Enable virtual terminal processing for better Unicode/emoji support
	h, _, _ := procGetStdHandle.Call(stdOutputHandle)
	var mode uint32
	procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode)))
	procSetConsoleMode.Call(h, uintptr(mode|enableProcessedOutput|enableVirtualTerminalProc))
}
