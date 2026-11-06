package task

// Getter — источник значений по ключу.
type Getter interface {
	Get(key string) (string, bool)
}

// MapGetter — источник на map. nil-значение ничего не находит.
type MapGetter map[string]string

// Get ищет ключ в map.
func (m MapGetter) Get(key string) (string, bool) {
	// TODO: реализуйте метод
	return "", false
}

// Chain опрашивает источники по порядку, nil-элементы пропускает.
type Chain []Getter

// Get возвращает первое найденное значение.
func (c Chain) Get(key string) (string, bool) {
	// TODO: реализуйте метод
	return "", false
}

// GetOr возвращает значение по ключу или def, если ключа нет.
func GetOr(g Getter, key, def string) string {
	// TODO: реализуйте функцию
	return ""
}

// TODO: добавьте проверки на этапе компиляции для MapGetter и Chain.
