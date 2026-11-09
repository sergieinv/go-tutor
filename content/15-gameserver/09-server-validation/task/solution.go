package task

import "math"

// Pos — позиция игрока на плоскости мира.
type Pos struct{ X, Z float64 }

// Verdict — решение по пакету перемещения.
type Verdict int

const (
	Accept Verdict = iota // принять новую позицию
	Reject                // отклонить и вернуть клиента на последнюю верную позицию
	Drop                  // лишний пакет за тик: проигнорировать
	Kick                  // отключить игрока
)

// Config — пределы проверки.
type Config struct {
	MaxSpeed        float64 // блоков за тик
	MaxCatchUp      int     // сколько тиков без движения можно засчитать в бюджет
	MaxCoord        float64 // |X| и |Z| больше — отключение
	MaxMovesPerTick int     // пакетов перемещения за один тик
}

// Validator проверяет перемещения одного игрока.
type Validator struct {
	cfg    Config
	pos    Pos    // последняя принятая позиция
	at     uint64 // тик, когда она принята
	tick   uint64 // тик, к которому относится счётчик
	moves  int    // пакетов в этом тике
	kicked bool
}

// NewValidator начинает проверку с позиции start на тике tick.
func NewValidator(cfg Config, start Pos, tick uint64) *Validator {
	return &Validator{cfg: cfg, pos: start, at: tick, tick: tick}
}

func (v *Validator) valid(p Pos) bool {
	for _, c := range [2]float64{p.X, p.Z} {
		if math.IsNaN(c) || math.IsInf(c, 0) || math.Abs(c) > v.cfg.MaxCoord {
			return false
		}
	}
	return true
}

// Move проверяет пакет перемещения в позицию to, пришедший на тике tick.
// Возвращает решение и позицию, в которой сервер считает игрока.
func (v *Validator) Move(tick uint64, to Pos) (Verdict, Pos) {
	if v.kicked {
		return Kick, v.pos
	}
	if !v.valid(to) {
		v.kicked = true
		return Kick, v.pos
	}
	if tick != v.tick {
		v.tick, v.moves = tick, 0
	}
	v.moves++
	if v.moves > v.cfg.MaxMovesPerTick {
		return Drop, v.pos
	}
	elapsed := min(max(tick-v.at, 1), uint64(v.cfg.MaxCatchUp))
	budget := v.cfg.MaxSpeed * float64(elapsed)
	dx, dz := to.X-v.pos.X, to.Z-v.pos.Z
	// Сравнение записано так, чтобы любое NaN давало отказ.
	if !(dx*dx+dz*dz <= budget*budget) {
		return Reject, v.pos
	}
	v.pos, v.at = to, tick
	return Accept, v.pos
}

// Teleport — перемещение по решению сервера (возрождение, команда).
func (v *Validator) Teleport(tick uint64, to Pos) {
	if v.kicked {
		return
	}
	v.pos, v.at = to, tick
}

// Position — последняя верная позиция.
func (v *Validator) Position() Pos { return v.pos }
