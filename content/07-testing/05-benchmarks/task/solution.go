package task

import (
	"strconv"
	"strings"
)

// AppendInts дописывает в dst числа nums через sep и возвращает
// расширенный срез.
func AppendInts(dst []byte, nums []int, sep string) []byte {
	for i, n := range nums {
		if i > 0 {
			dst = append(dst, sep...)
		}
		dst = strconv.AppendInt(dst, int64(n), 10)
	}
	return dst
}

// JoinInts склеивает числа nums через sep не более чем за одну аллокацию.
func JoinInts(nums []int, sep string) string {
	if len(nums) == 0 {
		return ""
	}
	size := len(sep) * (len(nums) - 1)
	for _, n := range nums {
		size += intLen(n)
	}
	var b strings.Builder
	b.Grow(size)
	// Временный буфер на стеке: 20 байт хватит на любое int64 со знаком.
	var tmp [20]byte
	for i, n := range nums {
		if i > 0 {
			b.WriteString(sep)
		}
		b.Write(strconv.AppendInt(tmp[:0], int64(n), 10))
	}
	return b.String()
}

// intLen — число символов в десятичной записи n, включая минус.
func intLen(n int) int {
	l := 1
	u := uint64(n)
	if n < 0 {
		l++
		u = -u // для math.MinInt тоже верно: модуль считаем в беззнаковом
	}
	for u >= 10 {
		u /= 10
		l++
	}
	return l
}
