package tools

import "testing"

func TestCoerceStringParam(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"hello", "hello"},
		{float64(42), "42"},
		{map[string]any{"text": "hi"}, `{"text":"hi"}`},
	}
	for _, tt := range tests {
		got := CoerceStringParam(tt.in)
		if got != tt.want {
			t.Errorf("CoerceStringParam(%#v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
