package task

// Config — конфигурация сервиса.
// Добавьте struct tags: ключи JSON — name, port, tags, debug.
type Config struct {
	Name  string
	Port  int
	Tags  []string
	Debug bool
}

// ParseConfig строго разбирает JSON-объект конфигурации.
func ParseConfig(data []byte) (Config, error) {
	// TODO: реализуйте функцию
	return Config{}, nil
}

// EncodeConfig кодирует конфигурацию в компактный JSON.
func EncodeConfig(c Config) ([]byte, error) {
	// TODO: реализуйте функцию
	return nil, nil
}
