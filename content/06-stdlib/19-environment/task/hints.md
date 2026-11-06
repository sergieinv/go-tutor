## Подсказка 1

Для четырёх переменных, где пустое значение равносильно отсутствию, хватит `os.Getenv` и маленькой функции `value(key, def string) string`: вернуть `def`, если `Getenv` дал `""`. А вот для `APP_TAGS` пустое и незаданное различаются — там нужен `os.LookupEnv`.

## Подсказка 2

Разбирайте по очереди и на первой ошибке выходите: `return Config{}, fmt.Errorf("APP_WORKERS: %w", err)`. Проверку диапазона (`> 0`) делайте отдельно после успешного разбора. Список: `strings.Split(raw, ",")`, затем `strings.TrimSpace` для каждого элемента и пропуск пустых.

## Подсказка 3

```go
raw, ok := os.LookupEnv("APP_TAGS")
if !ok {
	cfg.Tags = []string{"default"}
} else {
	for _, tag := range strings.Split(raw, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			cfg.Tags = append(cfg.Tags, tag)
		}
	}
}
```

Тесты используют `t.Setenv`, поэтому окружение после каждого теста восстанавливается само.
