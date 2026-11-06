package task

import (
	"slices"
	"testing"
)

func TestLeaderboardDoesNotModifyInput(t *testing.T) {
	in := []Player{{"c", 1, 3}, {"a", 3, 1}, {"b", 2, 2}}
	orig := slices.Clone(in)
	got := Leaderboard(in)
	if !slices.Equal(in, orig) {
		t.Fatalf("исходный срез изменён: %v, было %v", in, orig)
	}
	want := []Player{{"a", 3, 1}, {"b", 2, 2}, {"c", 1, 3}}
	if !slices.Equal(got, want) {
		t.Fatalf("Leaderboard = %v, want %v", got, want)
	}
	// Результат не должен делить массив со входом.
	got[0].Name = "changed"
	if in[1].Name != "a" {
		t.Errorf("результат разделяет массив с исходным срезом")
	}
}

func TestLeaderboardAllKeys(t *testing.T) {
	in := []Player{
		{"bob", 50, 120}, {"ann", 70, 200}, {"cat", 50, 100},
		{"dan", 50, 120}, {"eve", 70, 200}, {"abe", -5, 10},
		{"Bob", 50, 120},
	}
	want := []Player{
		{"ann", 70, 200}, {"eve", 70, 200}, {"cat", 50, 100},
		{"Bob", 50, 120}, {"bob", 50, 120}, {"dan", 50, 120},
		{"abe", -5, 10},
	}
	if got := Leaderboard(in); !slices.Equal(got, want) {
		t.Errorf("Leaderboard = %v, want %v", got, want)
	}
}

func TestLeaderboardLargeValues(t *testing.T) {
	const big = int(^uint(0) >> 1) // максимальный int
	in := []Player{{"low", -big, 1}, {"high", big, 1}, {"mid", 0, 1}}
	want := []Player{{"high", big, 1}, {"mid", 0, 1}, {"low", -big, 1}}
	if got := Leaderboard(in); !slices.Equal(got, want) {
		t.Errorf("Leaderboard = %v, want %v (сравнение вычитанием переполняется)", got, want)
	}
}

func TestLeaderboardEmpty(t *testing.T) {
	if got := Leaderboard(nil); len(got) != 0 {
		t.Errorf("Leaderboard(nil) = %v, want пустой срез", got)
	}
	if got := Leaderboard([]Player{}); len(got) != 0 {
		t.Errorf("Leaderboard([]) = %v, want пустой срез", got)
	}
}
