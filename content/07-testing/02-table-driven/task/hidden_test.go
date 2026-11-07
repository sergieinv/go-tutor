package task

import (
	"slices"
	"testing"
)

func leapRef(y int) bool { return y%4 == 0 && y%100 != 0 || y%400 == 0 }

func TestHiddenCasesAreCorrect(t *testing.T) {
	seen := map[string]bool{}
	for i, c := range LeapCases() {
		if c.Name == "" {
			t.Errorf("случай %d (год %d): пустое имя", i, c.Year)
		}
		if seen[c.Name] {
			t.Errorf("имя %q повторяется — подтесты станут %q и %q#01", c.Name, c.Name, c.Name)
		}
		seen[c.Name] = true
		if c.Year <= 0 {
			t.Errorf("случай %q: год %d должен быть положительным", c.Name, c.Year)
		}
		if want := leapRef(c.Year); c.Want != want {
			t.Errorf("случай %q: Want = %t, а год %d на самом деле %s", c.Name, c.Want, c.Year, map[bool]string{true: "високосный", false: "невисокосный"}[want])
		}
	}
}

func TestHiddenCasesCoverAllKinds(t *testing.T) {
	kinds := []struct {
		name string
		is   func(int) bool
	}{
		{"не делится на 4", func(y int) bool { return y%4 != 0 }},
		{"делится на 4, но не на 100", func(y int) bool { return y%4 == 0 && y%100 != 0 }},
		{"делится на 100, но не на 400", func(y int) bool { return y%100 == 0 && y%400 != 0 }},
		{"делится на 400", func(y int) bool { return y%400 == 0 }},
	}
	cases := LeapCases()
	for _, k := range kinds {
		if !slices.ContainsFunc(cases, func(c Case) bool { return k.is(c.Year) }) {
			t.Errorf("в таблице нет года вида «%s»", k.name)
		}
	}
}

func TestHiddenTableCatchesMutants(t *testing.T) {
	mutants := []struct {
		name string
		f    func(int) bool
	}{
		{"забыто правило 400", func(y int) bool { return y%4 == 0 && y%100 != 0 }},
		{"только правило 400", func(y int) bool { return y%400 == 0 }},
		{"всегда false", func(int) bool { return false }},
		{"всегда true", func(int) bool { return true }},
		{"перепутано 100 и 400", func(y int) bool { return y%4 == 0 && y%400 != 0 || y%100 == 0 }},
	}
	cases := LeapCases()
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			if got := Check(m.f, cases); len(got) == 0 {
				t.Errorf("таблица не отличила реализацию «%s» от правильной", m.name)
			}
		})
	}
}

func TestHiddenCheckOrderAndCalls(t *testing.T) {
	cases := []Case{
		{"a", 2001, true},
		{"b", 2004, true},
		{"c", 2100, true},
		{"d", 2400, false},
	}
	var calls []int
	f := func(y int) bool {
		calls = append(calls, y)
		return leapRef(y)
	}
	got := Check(f, cases)
	want := []string{
		"a: IsLeap(2001) = false, want true",
		"c: IsLeap(2100) = false, want true",
		"d: IsLeap(2400) = true, want false",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Check = %q,\nwant %q", got, want)
	}
	if !slices.Equal(calls, []int{2001, 2004, 2100, 2400}) {
		t.Errorf("f вызвана с годами %v, want [2001 2004 2100 2400] — по разу на случай, по порядку", calls)
	}
}

func TestHiddenCheckEmpty(t *testing.T) {
	called := false
	f := func(int) bool { called = true; return false }
	if got := Check(f, nil); got != nil {
		t.Errorf("Check(f, nil) = %q, want nil", got)
	}
	if got := Check(f, []Case{}); got != nil {
		t.Errorf("Check(f, []Case{}) = %q, want nil", got)
	}
	if called {
		t.Error("для пустой таблицы f вызываться не должна")
	}
	if got := Check(leapRef, []Case{{"ok", 1996, true}}); got != nil {
		t.Errorf("Check без расхождений = %q, want nil (не пустой срез)", got)
	}
}
