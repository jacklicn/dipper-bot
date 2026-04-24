//go:build !windows

package win32

// SetConsoleUTF8 is a no-op on non-Windows.
func SetConsoleUTF8() {}
