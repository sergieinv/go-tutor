package task

import (
	"math"
	"strconv"
	"strings"
)

// ParseSize разбирает размер вида "512", "10K", "3M", "2G" в байты.
// Синтаксические ошибки оборачивают strconv.ErrSyntax,
// переполнение — strconv.ErrRange.
func ParseSize(s string) (int64, error) {
	num, mult := s, int64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		num, mult = s[:len(s)-1], 1<<10
	case strings.HasSuffix(s, "M"):
		num, mult = s[:len(s)-1], 1<<20
	case strings.HasSuffix(s, "G"):
		num, mult = s[:len(s)-1], 1<<30
	}
	// ParseUint не принимает знак; bitSize 63 — ровно диапазон
	// неотрицательных int64. С base 10 символы "_" запрещены.
	n, err := strconv.ParseUint(num, 10, 63)
	if err != nil {
		// err уже *strconv.NumError с ErrSyntax или ErrRange.
		return 0, err
	}
	if int64(n) > math.MaxInt64/mult {
		return 0, &strconv.NumError{Func: "ParseSize", Num: s, Err: strconv.ErrRange}
	}
	return int64(n) * mult, nil
}
