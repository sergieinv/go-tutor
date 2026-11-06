package task

import "testing"

func TestNormalizeEdge(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"http://ex.com:443/", "http://ex.com:443/"},
		{"https://ex.com:80/", "https://ex.com:80/"},
		{"HtTpS://USER:pw@Ex.Com/Path/Case?", "https://ex.com/Path/Case"},
		{"http://ex.com/?tag=b&tag=a&a=", "http://ex.com/?a=&tag=b&tag=a"},
		{"http://ex.com/s?q=go+lang&z=%26", "http://ex.com/s?q=go+lang&z=%26"},
		{"http://ex.com/a/../b/", "http://ex.com/a/../b/"},
		{"http://ex.com#frag", "http://ex.com/"},
		{"https://ex.com:8080", "https://ex.com:8080/"},
	}
	for _, tt := range tests {
		got, err := Normalize(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestNormalizeMoreErrors(t *testing.T) {
	bad := []string{
		"", "/relative/path", "localhost:8080", "http:///path", "mailto:a@b.c",
		"http://[::1", "http://ex.com/%zz", "javascript:alert(1)", "https://:443/",
	}
	for _, in := range bad {
		if got, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) = %q, nil; want ошибку", in, got)
		}
	}
}

func TestNormalizeIdempotent(t *testing.T) {
	in := "HTTPS://Ex.com:443/a%2Fb/c?z=1&y=%D1%8F&y=2#x"
	once, err := Normalize(in)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Normalize(once)
	if err != nil || twice != once {
		t.Errorf("Normalize(Normalize(x)) = %q, %v; want %q", twice, err, once)
	}
}
