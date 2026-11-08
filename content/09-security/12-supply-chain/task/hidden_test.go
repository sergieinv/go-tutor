package task

import (
	"slices"
	"testing"
)

const header = "module example.com/app\n\ngo 1.25.0\n\n"

func TestCompareVersionsPrerelease(t *testing.T) {
	// Порядок из semver.org, раздел 11, плюс версии модулей Go.
	ordered := []string{
		"v0.0.0-20210101000000-abcdefabcdef",
		"v0.0.0-20220101000000-abcdefabcdef",
		"v0.3.7",
		"v0.3.8-0.20220101000000-abcdefabcdef",
		"v0.3.8",
		"v1.0.0-alpha",
		"v1.0.0-alpha.1",
		"v1.0.0-alpha.beta",
		"v1.0.0-beta",
		"v1.0.0-beta.2",
		"v1.0.0-beta.11",
		"v1.0.0-rc.1",
		"v1.0.0",
		"v1.2.9",
		"v1.2.10",
		"v1.10.0",
		"v2.0.0+incompatible",
		"v2.1.0",
	}
	for i, a := range ordered {
		for j, b := range ordered {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := CompareVersions(a, b); got != want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", a, b, got, want)
			}
		}
	}
	if got := CompareVersions("v2.0.0+incompatible", "v2.0.0"); got != 0 {
		t.Errorf("метаданные сборки не участвуют в сравнении: got %d, want 0", got)
	}
}

func TestExploitVersionAsString(t *testing.T) {
	db := []Vuln{
		{ID: "GO-2099-0001", Module: "example.com/a", Fixed: "v0.10.0"},
		{ID: "GO-2099-0002", Module: "example.com/b", Introduced: "v1.2.0", Fixed: "v1.2.10"},
	}
	gomod := header + "require example.com/a v0.9.5\nrequire example.com/b v1.2.9\n"
	want := []string{"GO-2099-0001 example.com/a@v0.9.5", "GO-2099-0002 example.com/b@v1.2.9"}
	if got := audit(t, gomod, db); !slices.Equal(got, want) {
		t.Fatalf("Audit = %q, want %q — версии сравниваются как строки?", got, want)
	}
}

func TestExploitPrereleaseOfFixed(t *testing.T) {
	db := []Vuln{{ID: "GO-2099-0003", Module: "example.com/c", Introduced: "v1.0.0", Fixed: "v1.4.0"}}
	gomod := header + "require example.com/c v1.4.0-rc.1\n"
	want := []string{"GO-2099-0003 example.com/c@v1.4.0-rc.1"}
	if got := audit(t, gomod, db); !slices.Equal(got, want) {
		t.Fatalf("Audit = %q, want %q — v1.4.0-rc.1 вышла раньше исправления v1.4.0", got, want)
	}
}

func TestExploitPseudoVersion(t *testing.T) {
	gomod := header + "require golang.org/x/text v0.3.8-0.20220101000000-abcdefabcdef\n"
	want := []string{"GO-2022-1059 golang.org/x/text@v0.3.8-0.20220101000000-abcdefabcdef"}
	if got := audit(t, gomod, []Vuln{textVuln}); !slices.Equal(got, want) {
		t.Fatalf("Audit = %q, want %q — псевдоверсия коммита до v0.3.8", got, want)
	}
}

func TestExploitRequireBlock(t *testing.T) {
	gomod := header + `require (
	example.com/ok v1.0.0
	golang.org/x/text v0.3.7 // indirect
)
`
	want := []string{"GO-2022-1059 golang.org/x/text@v0.3.7"}
	if got := audit(t, gomod, []Vuln{textVuln}); !slices.Equal(got, want) {
		t.Fatalf("Audit = %q, want %q — уязвимость в блоке require", got, want)
	}
}

func TestExploitReplaceBlock(t *testing.T) {
	gomod := header + `require golang.org/x/crypto v0.55.0

replace (
	example.com/lib => ../lib
	golang.org/x/crypto v0.55.0 => github.com/evil/crypto v0.55.0
)
`
	want := []string{"replace golang.org/x/crypto"}
	if got := audit(t, gomod, nil, "example.com/lib"); !slices.Equal(got, want) {
		t.Fatalf("Audit = %q, want %q — подмена модуля в блоке replace", got, want)
	}
}

func TestAuditFull(t *testing.T) {
	gomod := `// Сервис заказов
module example.com/orders

go 1.25.0

toolchain go1.25.4

require (
	example.com/x v1.5.0
	golang.org/x/text v0.3.7
)

require (
	example.com/y v2.0.0+incompatible // indirect
	example.com/z v0.1.0 // indirect
)

exclude example.com/x v1.4.0

retract (
	v1.0.1 // опубликовано по ошибке
	[v1.1.0, v1.1.5]
)

// replace example.com/z => ./z — закомментировано, не считается
replace example.com/x v1.5.0 => ./x
`
	db := []Vuln{
		textVuln,
		{ID: "GO-2099-0010", Module: "example.com/x", Introduced: "v1.5.0"},                  // не исправлена
		{ID: "GO-2099-0011", Module: "example.com/x", Fixed: "v1.5.0"},                       // уже исправлена
		{ID: "GO-2099-0012", Module: "example.com/y", Introduced: "v2.0.0", Fixed: "v2.1.0"}, // +incompatible
		{ID: "GO-2099-0013", Module: "example.com/zz", Fixed: "v9.0.0"},                      // другой модуль
		{ID: "GO-2099-0014", Module: "example.com/z", Introduced: "v0.2.0"},                  // ещё не появилась
		{ID: "GO-2022-1059", Module: "golang.org/x/text", Fixed: "v0.3.8"},                   // дубль записи
	}
	want := []string{
		"GO-2022-1059 golang.org/x/text@v0.3.7",
		"GO-2099-0010 example.com/x@v1.5.0",
		"GO-2099-0012 example.com/y@v2.0.0+incompatible",
		"replace example.com/x",
	}
	if got := audit(t, gomod, db); !slices.Equal(got, want) {
		t.Fatalf("Audit = %q\nwant %q", got, want)
	}
}

func TestAuditEmpty(t *testing.T) {
	got, err := Audit(header, []Vuln{textVuln}, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("Audit без зависимостей = %q, %v; want пусто, nil", got, err)
	}
}

func TestAuditMalformed(t *testing.T) {
	for _, gomod := range []string{
		header + "require golang.org/x/text\n",
		header + "require (\n\tgolang.org/x/text v0.3.7 extra\n)\n",
		header + "require (\n\tgolang.org/x/text v0.3.7\n",
		header + "replace golang.org/x/text ../text\n",
	} {
		if got, err := Audit(gomod, []Vuln{textVuln}, nil); err == nil {
			t.Errorf("Audit(%q) = %q, nil; want ошибку", gomod, got)
		}
	}
}
