package task

// Bloom — фильтр Блума для проверки «этот ключ мы, возможно, видели».
// Устройство и формулы — в условии задачи.
type Bloom struct {
	// TODO: битовый массив и параметры.
}

// NewBloom создаёт фильтр на n ключей с долей ложных срабатываний p.
func NewBloom(n int, p float64) *Bloom {
	// TODO
	return &Bloom{}
}

// Bits — число бит m в фильтре.
func (b *Bloom) Bits() int {
	// TODO
	return 0
}

// K — число хеш-функций k.
func (b *Bloom) K() int {
	// TODO
	return 0
}

// Add добавляет ключ.
func (b *Bloom) Add(key string) {
	// TODO
}

// MayContain сообщает, мог ли ключ быть добавлен: false — точно нет,
// true — возможно.
func (b *Bloom) MayContain(key string) bool {
	// TODO
	return false
}
