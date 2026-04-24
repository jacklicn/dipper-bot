//go:build !windows

package unix

// SetConsoleUTF8 is a no-op on non-Windows.
func SetConsoleUTF8() {}
