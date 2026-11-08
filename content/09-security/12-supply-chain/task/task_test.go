package task

import (
	"slices"
	"testing"
)

var textVuln = Vuln{ID: "GO-2022-1059", Module: "golang.org/x/text", Fixed: "v0.3.8"}

func audit(t *testing.T, gomod string, db []Vuln, allow ...string) []string {
	t.Helper()
	got, err := Audit(gomod, db, allow)
	if err != nil {
		t.Fatalf("Audit: %v\ngo.mod:\n%s", err, gomod)
	}
	return got
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "v1.2.3", 0},
		{"v1.2.3", "v1.2.4", -1},
		{"v1.3.0", "v1.2.9", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v0.3.7", "v0.3.8", -1},
	}
	for _, tt := range tests {
		if got := CompareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestAuditSingleRequire(t *testing.T) {
	gomod := "module example.com/app\n\ngo 1.25.0\n\nrequire golang.org/x/text v0.3.7\n"
	want := []string{"GO-2022-1059 golang.org/x/text@v0.3.7"}
	if got := audit(t, gomod, []Vuln{textVuln}); !slices.Equal(got, want) {
		t.Fatalf("Audit = %q, want %q", got, want)
	}
}

func TestAuditFixedVersionClean(t *testing.T) {
	gomod := "module example.com/app\n\ngo 1.25.0\n\nrequire golang.org/x/text v0.3.8\n"
	if got := audit(t, gomod, []Vuln{textVuln}); len(got) != 0 {
		t.Fatalf("Audit = %q, want пусто: v0.3.8 исправлена", got)
	}
}

func TestAuditReplace(t *testing.T) {
	gomod := `module example.com/app

go 1.25.0

require example.com/lib v1.0.0

replace example.com/lib => ../lib
replace golang.org/x/net => github.com/fork/net v0.1.0
`
	want := []string{"replace golang.org/x/net"}
	if got := audit(t, gomod, nil, "example.com/lib"); !slices.Equal(got, want) {
		t.Fatalf("Audit = %q, want %q (example.com/lib разрешён)", got, want)
	}
}
