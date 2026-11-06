package task

// ParseSize разбирает размер вида "512", "10K", "3M", "2G" в байты.
// Синтаксические ошибки оборачивают strconv.ErrSyntax,
// переполнение — strconv.ErrRange.
func ParseSize(s string) (int64, error) {
	// TODO: реализуйте функцию
	return 0, nil
}
