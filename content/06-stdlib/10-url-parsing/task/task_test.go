package task

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"HTTP://Example.COM:80?b=2&a=1#top", "http://example.com/?a=1&b=2"},
		{"https://ex.com:8443/a%20b", "https://ex.com:8443/a%20b"},
		{"https://ex.com:443/x", "https://ex.com/x"},
		{"http://ex.com", "http://ex.com/"},
	}
	for _, tt := range tests {
		got, err := Normalize(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestNormalizeErrors(t *testing.T) {
	for _, in := range []string{"ftp://ex.com/", "example.com/path"} {
		if got, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) = %q, nil; want ошибку", in, got)
		}
	}
}
