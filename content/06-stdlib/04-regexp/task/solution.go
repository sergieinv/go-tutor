package task

import "regexp"

// Компилируем один раз, на уровне пакета.
var placeholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Expand заменяет плейсхолдеры ${name} значениями из vars.
// Неизвестные плейсхолдеры остаются как есть.
func Expand(s string, vars map[string]string) string {
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		// Функция получает всё совпадение "${name}" — имя
		// выделяем сами: убираем "${" и "}".
		name := m[2 : len(m)-1]
		if v, ok := vars[name]; ok {
			return v // результат функции вставляется буквально
		}
		return m
	})
}

// Vars возвращает имена плейсхолдеров в порядке первого появления, без повторов.
func Vars(s string) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	return names
}
