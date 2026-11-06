package task

import "math"

// Percent возвращает, сколько процентов part составляет
// от total, с округлением до одного знака после запятой
// через math.Round. При total <= 0 — 0.
func Percent(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	// В float64 переводим до деления, иначе 1/3 == 0.
	p := float64(part) * 100 / float64(total)
	return math.Round(p*10) / 10
}

// AddSat складывает a и b с насыщением: если сумма
// не помещается в uint8, результат 255.
func AddSat(a, b uint8) uint8 {
	// a + b в uint8 уже завернулась бы — считаем в int.
	sum := int(a) + int(b)
	if sum > math.MaxUint8 {
		return math.MaxUint8
	}
	return uint8(sum)
}
