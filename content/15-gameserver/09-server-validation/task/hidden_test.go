package task

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestHiddenExactBudgetBoundary(t *testing.T) {
	c := Config{MaxSpeed: 5, MaxCatchUp: 3, MaxCoord: 1000, MaxMovesPerTick: 5}
	v := NewValidator(c, Pos{0, 0}, 0)
	check(t, v, 1, Pos{3, 4}, Accept, Pos{3, 4}) // ровно 5: граница включена
	check(t, v, 2, Pos{6, 8.0001}, Reject, Pos{3, 4})
	check(t, v, 2, Pos{-1, 1}, Accept, Pos{-1, 1}) // ровно 5 от (3,4)
}

func TestHiddenSameTickAsStart(t *testing.T) {
	v := NewValidator(cfg, Pos{0, 0}, 7)
	// Пакет в том же тике, что и старт: бюджет как за один тик.
	check(t, v, 7, Pos{1, 0}, Accept, Pos{1, 0})
	check(t, v, 7, Pos{2, 0}, Accept, Pos{2, 0})
	check(t, v, 7, Pos{2.5, 0}, Drop, Pos{2, 0})
}

func TestHiddenCatchUpAfterLag(t *testing.T) {
	v := NewValidator(cfg, Pos{0, 0}, 0)
	check(t, v, 4, Pos{4, 0}, Accept, Pos{4, 0})   // 4 тика — бюджет 4
	check(t, v, 104, Pos{9, 0}, Accept, Pos{9, 0}) // 100 тиков, засчитано 5
	check(t, v, 204, Pos{14.01, 0}, Reject, Pos{9, 0})
}

func TestHiddenRejectDoesNotAdvanceAnchor(t *testing.T) {
	v := NewValidator(cfg, Pos{0, 0}, 0)
	check(t, v, 1, Pos{3, 0}, Reject, Pos{0, 0})
	check(t, v, 2, Pos{2.5, 0}, Reject, Pos{0, 0})
	// Принятая позиция — с тика 0: к тику 3 бюджет 3.
	check(t, v, 3, Pos{3, 0}, Accept, Pos{3, 0})
	// Теперь отсчёт от тика 3.
	check(t, v, 4, Pos{4.5, 0}, Reject, Pos{3, 0})
}

func TestHiddenDropCountsAllPacketsAndResets(t *testing.T) {
	c := cfg
	c.MaxMovesPerTick = 3
	v := NewValidator(c, Pos{0, 0}, 0)
	check(t, v, 1, Pos{50, 0}, Reject, Pos{0, 0}) // отклонённый тоже считается
	check(t, v, 1, Pos{0.5, 0}, Accept, Pos{0.5, 0})
	check(t, v, 1, Pos{0.6, 0}, Accept, Pos{0.6, 0})
	check(t, v, 1, Pos{0.7, 0}, Drop, Pos{0.6, 0})
	check(t, v, 1, Pos{0.8, 0}, Drop, Pos{0.6, 0})
	check(t, v, 2, Pos{0.9, 0}, Accept, Pos{0.9, 0})
	check(t, v, 2, Pos{1.0, 0}, Accept, Pos{1.0, 0})
	check(t, v, 2, Pos{1.1, 0}, Accept, Pos{1.1, 0})
	check(t, v, 2, Pos{1.2, 0}, Drop, Pos{1.1, 0})
	check(t, v, 9, Pos{1.3, 0}, Accept, Pos{1.3, 0})
}

func TestHiddenInvalidNumbers(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	cases := []struct {
		name string
		to   Pos
	}{
		{"NaN в X", Pos{nan, 0}},
		{"NaN в Z", Pos{0, nan}},
		{"+Inf", Pos{inf, 0}},
		{"-Inf в Z", Pos{0, -inf}},
		{"оба NaN", Pos{nan, nan}},
		{"за границей мира", Pos{3e7 + 1, 0}},
		{"за границей мира по Z", Pos{0, -3e7 - 0.5}},
		{"огромное число", Pos{-1e308, 1e308}},
		{"наименьшее отрицательное", Pos{-math.MaxFloat64, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewValidator(cfg, Pos{2, 3}, 0)
			check(t, v, 1, c.to, Kick, Pos{2, 3})
		})
	}
}

func TestHiddenBorderIsAllowed(t *testing.T) {
	c := Config{MaxSpeed: 1, MaxCatchUp: 5, MaxCoord: 100, MaxMovesPerTick: 2}
	v := NewValidator(c, Pos{99.5, -99.5}, 0)
	check(t, v, 1, Pos{100, -100}, Accept, Pos{100, -100}) // |x| = MaxCoord — можно
	check(t, v, 2, Pos{100.25, -100}, Kick, Pos{100, -100})
}

func TestHiddenKickIsFinal(t *testing.T) {
	v := NewValidator(cfg, Pos{0, 0}, 0)
	check(t, v, 1, Pos{0.5, 0}, Accept, Pos{0.5, 0})
	check(t, v, 2, Pos{math.NaN(), 0}, Kick, Pos{0.5, 0})
	check(t, v, 3, Pos{0.6, 0}, Kick, Pos{0.5, 0})
	v.Teleport(4, Pos{10, 10})
	check(t, v, 5, Pos{10, 10}, Kick, Pos{0.5, 0})
}

func TestHiddenKickBeforeDrop(t *testing.T) {
	c := cfg
	c.MaxMovesPerTick = 1
	v := NewValidator(c, Pos{0, 0}, 0)
	check(t, v, 1, Pos{0.1, 0}, Accept, Pos{0.1, 0})
	check(t, v, 1, Pos{0.2, 0}, Drop, Pos{0.1, 0})
	check(t, v, 1, Pos{math.Inf(-1), 0}, Kick, Pos{0.1, 0})
}

func TestHiddenTeleport(t *testing.T) {
	v := NewValidator(cfg, Pos{0, 0}, 0)
	check(t, v, 1, Pos{0.5, 0}, Accept, Pos{0.5, 0})
	v.Teleport(2, Pos{500, -500})
	if v.Position() != (Pos{500, -500}) {
		t.Fatalf("после Teleport Position() = %v", v.Position())
	}
	// Клиент ещё не знает о телепорте и шлёт старые координаты.
	check(t, v, 2, Pos{0.6, 0}, Reject, Pos{500, -500})
	check(t, v, 3, Pos{500.5, -500}, Accept, Pos{500.5, -500})
}

func TestHiddenNaNInStartIsNotAccepted(t *testing.T) {
	// Даже если где-то в расчёте получится NaN, принимать нельзя:
	// стартовая позиция испорчена, расстояние — NaN.
	v := NewValidator(cfg, Pos{math.NaN(), 0}, 0)
	got, _ := v.Move(1, Pos{0, 0})
	if got == Accept {
		t.Fatalf("расстояние NaN: Move = Accept, want не Accept")
	}
}

// Свойство: что бы ни слал клиент, позиция сервера конечна, в
// пределах мира, а принятые шаги не быстрее бюджета.
func TestHiddenRandomClientNeverEscapes(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 9))
	c := Config{MaxSpeed: 0.8, MaxCatchUp: 4, MaxCoord: 1000, MaxMovesPerTick: 3}
	for game := range 200 {
		v := NewValidator(c, Pos{0, 0}, 0)
		tick := uint64(0)
		last, lastAt := Pos{0, 0}, uint64(0)
		perTick := 0
		for range 300 {
			if rng.IntN(3) == 0 {
				tick += uint64(rng.IntN(8))
				perTick = 0
			}
			p := v.Position()
			to := Pos{p.X + rng.NormFloat64()*1.5, p.Z + rng.NormFloat64()*1.5}
			if rng.IntN(500) == 0 {
				to.X = math.NaN()
			}
			got, pos := v.Move(tick, to)
			if got == Kick {
				if !math.IsNaN(to.X) && math.Abs(to.X) <= 1000 && math.Abs(to.Z) <= 1000 {
					t.Fatalf("игра %d: Kick на корректных числах %v", game, to)
				}
				break
			}
			perTick++
			if perTick > 3 && got != Drop {
				t.Fatalf("игра %d: %d-й пакет за тик — %s, want Drop", game, perTick, names(got))
			}
			if got == Accept {
				e := float64(min(max(tick-lastAt, 1), 4))
				dx, dz := to.X-last.X, to.Z-last.Z
				if dx*dx+dz*dz > (0.8*e)*(0.8*e) {
					t.Fatalf("игра %d: принят шаг длиннее бюджета", game)
				}
				last, lastAt = to, tick
			}
			if pos != last || math.IsNaN(pos.X) || math.Abs(pos.X) > 1000 || math.Abs(pos.Z) > 1000 {
				t.Fatalf("игра %d: позиция сервера %v, want %v", game, pos, last)
			}
		}
	}
}
