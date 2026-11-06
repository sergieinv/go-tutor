package task

import "testing"

func TestSeasonAllMonths(t *testing.T) {
	want := map[int]string{
		12: "winter", 1: "winter", 2: "winter",
		3: "spring", 5: "spring",
		6: "summer", 8: "summer",
		9: "autumn", 11: "autumn",
		0: "invalid", 13: "invalid", -1: "invalid",
	}
	for m, w := range want {
		if got := Season(m); got != w {
			t.Errorf("Season(%d) = %q, want %q", m, got, w)
		}
	}
}

func TestStatusClassEdges(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{100, "informational"},
		{199, "informational"},
		{299, "success"},
		{399, "redirect"},
		{400, "client error"},
		{429, "rate limited"},
		{428, "client error"},
		{499, "client error"},
		{500, "server error"},
		{599, "server error"},
		{99, "unknown"},
		{600, "unknown"},
		{0, "unknown"},
		{-200, "unknown"},
	}
	for _, tt := range tests {
		if got := StatusClass(tt.code); got != tt.want {
			t.Errorf("StatusClass(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}
