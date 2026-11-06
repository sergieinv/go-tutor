package task

// Swap меняет местами значения *a и *b.
// Если a или b равен nil, ничего не делает.
func Swap(a, b *int) {
	if a == nil || b == nil {
		return
	}
	*a, *b = *b, *a
}

// MaxPtr возвращает указатель на первый максимальный
// элемент самого среза s. Для пустого s — nil.
func MaxPtr(s []int) *int {
	if len(s) == 0 {
		return nil
	}
	best := 0
	for i, v := range s {
		if v > s[best] {
			best = i
		}
	}
	// &s[best] — адрес элемента массива под срезом.
	// Адрес переменной цикла v был бы адресом копии.
	return &s[best]
}
