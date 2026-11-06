## Подсказка 1

Функции регистрируются до разбора: `template.New("x").Funcs(template.FuncMap{...}).Parse(text)`. Строгий режим для map — `.Option("missingkey=error")`, тоже до `Execute`.

## Подсказка 2

Конвейер `{{.Tags | join ", "}}` вызывает `join(", ", .Tags)` — значение слева становится **последним** аргументом. Поэтому `strings.Join` напрямую не подходит: нужна обёртка `func(sep string, items []string) string`. Так же устроен `default`: `func(def, v string) string`.

## Подсказка 3

`Execute` пишет в writer по мере исполнения, и при ошибке там останется начало текста. Исполняйте в буфер и возвращайте его содержимое только при успехе:

```go
var buf bytes.Buffer
if err := t.Execute(&buf, data); err != nil {
	return "", err
}
return buf.String(), nil
```
