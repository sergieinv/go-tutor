package task

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
	// TODO
}

// NewValidator начинает проверку с позиции start на тике tick.
func NewValidator(cfg Config, start Pos, tick uint64) *Validator {
	return &Validator{}
}

// Move проверяет пакет перемещения в позицию to, пришедший на тике tick.
// Возвращает решение и позицию, в которой сервер считает игрока.
func (v *Validator) Move(tick uint64, to Pos) (Verdict, Pos) {
	// TODO
	return Accept, to
}

// Teleport — перемещение по решению сервера (возрождение, команда).
func (v *Validator) Teleport(tick uint64, to Pos) {
	// TODO
}

// Position — последняя верная позиция.
func (v *Validator) Position() Pos {
	// TODO
	return Pos{}
}
