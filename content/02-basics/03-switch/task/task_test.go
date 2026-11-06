package task

import "testing"

func TestSeason(t *testing.T) {
	tests := []struct {
		month int
		want  string
	}{
		{1, "winter"},
		{4, "spring"},
		{7, "summer"},
		{10, "autumn"},
	}
	for _, tt := range tests {
		if got := Season(tt.month); got != tt.want {
			t.Errorf("Season(%d) = %q, want %q", tt.month, got, tt.want)
		}
	}
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{200, "success"},
		{301, "redirect"},
		{404, "client error"},
		{503, "server error"},
	}
	for _, tt := range tests {
		if got := StatusClass(tt.code); got != tt.want {
			t.Errorf("StatusClass(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}
