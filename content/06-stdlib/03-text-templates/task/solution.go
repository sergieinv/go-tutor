package task

import (
	"bytes"
	"strings"
	"text/template"
)

var funcs = template.FuncMap{
	"upper": strings.ToUpper,
	// Срез — последним аргументом: так работает конвейер.
	"join": func(sep string, items []string) string {
		return strings.Join(items, sep)
	},
	"default": func(def, v string) string {
		if v == "" {
			return def
		}
		return v
	},
}

// Render разбирает шаблон text с функциями upper, join и default
// и исполняет его с данными data. При ошибке возвращает "" и ошибку.
func Render(text string, data any) (string, error) {
	t, err := template.New("render").Funcs(funcs).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", err
	}
	// Буфер вместо прямого вывода: при ошибке нет частичного результата.
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
