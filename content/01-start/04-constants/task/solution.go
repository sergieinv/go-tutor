package task

import "fmt"

// Нетипизированные константы: подходят и для сравнения
// с int64, и для деления float64 без преобразований.
const (
	KiB = 1 << 10
	MiB = KiB << 10
	GiB = MiB << 10
	TiB = GiB << 10
)

// HumanSize форматирует размер в байтах: "1023 B", "1.5 KiB",
// "1.0 MiB". Выбирается наибольшая единица до TiB включительно,
// не превосходящая |n|; дробная часть — через %.1f.
func HumanSize(n int64) string {
	// Модуль считаем в uint64: -n для math.MinInt64 переполнится
	// и останется отрицательным, а в uint64 2⁶³ помещается.
	sign, u := "", uint64(n)
	if n < 0 {
		sign, u = "-", -u
	}
	switch {
	case u >= TiB:
		return fmt.Sprintf("%s%.1f TiB", sign, float64(u)/TiB)
	case u >= GiB:
		return fmt.Sprintf("%s%.1f GiB", sign, float64(u)/GiB)
	case u >= MiB:
		return fmt.Sprintf("%s%.1f MiB", sign, float64(u)/MiB)
	case u >= KiB:
		return fmt.Sprintf("%s%.1f KiB", sign, float64(u)/KiB)
	}
	return fmt.Sprintf("%s%d B", sign, u)
}
