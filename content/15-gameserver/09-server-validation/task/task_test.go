package task

import (
	"math"
	"testing"
)

var cfg = Config{MaxSpeed: 1, MaxCatchUp: 5, MaxCoord: 3e7, MaxMovesPerTick: 2}

func names(v Verdict) string {
	switch v {
	case Accept:
		return "Accept"
	case Reject:
		return "Reject"
	case Drop:
		return "Drop"
	case Kick:
		return "Kick"
	}
	return "?"
}

// check вызывает Move и сверяет решение и позицию сервера.
func check(t *testing.T, v *Validator, tick uint64, to Pos, want Verdict, wantPos Pos) {
	t.Helper()
	got, pos := v.Move(tick, to)
	if got != want || pos != wantPos {
		t.Fatalf("тик %d: Move(%v) = %s, %v; want %s, %v", tick, to, names(got), pos, names(want), wantPos)
	}
	if v.Position() != wantPos {
		t.Fatalf("тик %d: Position() = %v, want %v", tick, v.Position(), wantPos)
	}
}

func TestNormalWalk(t *testing.T) {
	v := NewValidator(cfg, Pos{0, 0}, 100)
	check(t, v, 101, Pos{0.3, 0}, Accept, Pos{0.3, 0})
	check(t, v, 102, Pos{0.6, 0.2}, Accept, Pos{0.6, 0.2})
	check(t, v, 103, Pos{0.6, 1.1}, Accept, Pos{0.6, 1.1})
}

func TestTooFastIsRejected(t *testing.T) {
	v := NewValidator(cfg, Pos{0, 0}, 100)
	check(t, v, 101, Pos{5, 0}, Reject, Pos{0, 0})
	check(t, v, 102, Pos{0.5, 0}, Accept, Pos{0.5, 0})
}

func TestInvalidNumbersKick(t *testing.T) {
	for _, to := range []Pos{{math.NaN(), 0}, {0, math.Inf(1)}, {1e308, 0}} {
		v := NewValidator(cfg, Pos{1, 1}, 0)
		check(t, v, 1, to, Kick, Pos{1, 1})
	}
}

func TestTooManyPacketsPerTick(t *testing.T) {
	v := NewValidator(cfg, Pos{0, 0}, 10)
	check(t, v, 11, Pos{0.1, 0}, Accept, Pos{0.1, 0})
	check(t, v, 11, Pos{0.2, 0}, Accept, Pos{0.2, 0})
	check(t, v, 11, Pos{0.3, 0}, Drop, Pos{0.2, 0})
	check(t, v, 12, Pos{0.3, 0}, Accept, Pos{0.3, 0})
}
