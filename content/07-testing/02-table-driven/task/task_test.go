package task

import (
	"slices"
	"testing"
)

// Таблица из задания, прогнанная обычными подтестами.
func TestLeapTable(t *testing.T) {
	for _, c := range LeapCases() {
		t.Run(c.Name, func(t *testing.T) {
			if got := IsLeap(c.Year); got != c.Want {
				t.Errorf("IsLeap(%d) = %t, want %t", c.Year, got, c.Want)
			}
		})
	}
}

func TestLeapCasesNotEmpty(t *testing.T) {
	if len(LeapCases()) < 4 {
		t.Fatalf("в таблице %d случаев, нужно хотя бы 4 — по одному на каждый вид года", len(LeapCases()))
	}
}

func TestCheckReportsMismatch(t *testing.T) {
	cases := []Case{
		{"двухтысячный", 2000, true},
		{"девятисотый", 1900, false},
	}
	alwaysFalse := func(int) bool { return false }
	got := Check(alwaysFalse, cases)
	want := []string{"двухтысячный: IsLeap(2000) = false, want true"}
	if !slices.Equal(got, want) {
		t.Errorf("Check(alwaysFalse, cases) = %q, want %q", got, want)
	}
}

func TestCheckCorrectImplementation(t *testing.T) {
	if got := Check(IsLeap, LeapCases()); got != nil {
		t.Errorf("Check(IsLeap, LeapCases()) = %q, want nil — таблица расходится с правильной реализацией", got)
	}
}

// Ошибочная реализация: забыты правила 100 и 400.
func TestTableCatchesDivBy4Only(t *testing.T) {
	naive := func(y int) bool { return y%4 == 0 }
	if got := Check(naive, LeapCases()); len(got) == 0 {
		t.Error("таблица не заметила реализацию y%4 == 0")
	}
}
