//go:build windows

package unix

// SetConsoleUTF8 is a no-op on Windows.
func SetConsoleUTF8() {}
