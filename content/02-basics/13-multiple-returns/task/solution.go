package task

import (
	"fmt"
	"strconv"
	"strings"
)

// MinMax возвращает минимум и максимум nums.
// Для пустого среза — 0, 0, false.
func MinMax(nums []int) (lo, hi int, ok bool) {
	if len(nums) == 0 {
		return 0, 0, false
	}
	lo, hi = nums[0], nums[0]
	for _, v := range nums[1:] {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	return lo, hi, true
}

// ParsePoint разбирает строку вида "3,-4" в координаты.
// Пробелы вокруг чисел допустимы. При ошибке — 0, 0 и error.
func ParsePoint(s string) (x, y int, err error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("ожидалось \"x,y\", получено %q", s)
	}
	// Отдельные переменные, чтобы при ошибке второй части
	// не вернуть уже разобранный x.
	px, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	py, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, err
	}
	return px, py, nil
}
