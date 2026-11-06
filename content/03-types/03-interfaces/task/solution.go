package task

// Getter — источник значений по ключу.
type Getter interface {
	Get(key string) (string, bool)
}

// MapGetter — источник на map. nil-значение ничего не находит.
type MapGetter map[string]string

// Get ищет ключ в map. Чтение из nil-map безопасно.
func (m MapGetter) Get(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

// Chain опрашивает источники по порядку, nil-элементы пропускает.
type Chain []Getter

// Get возвращает первое найденное значение.
func (c Chain) Get(key string) (string, bool) {
	for _, g := range c {
		if g == nil {
			continue
		}
		if v, ok := g.Get(key); ok {
			return v, true
		}
	}
	return "", false
}

// GetOr возвращает значение по ключу или def, если ключа нет.
func GetOr(g Getter, key, def string) string {
	if v, ok := g.Get(key); ok {
		return v
	}
	return def
}

// Сборка упадёт, если сигнатуры перестанут совпадать.
var (
	_ Getter = MapGetter(nil)
	_ Getter = Chain(nil)
)
