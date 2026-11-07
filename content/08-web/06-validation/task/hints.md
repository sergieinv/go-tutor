## Подсказка 1

Заведите `errs := FieldErrors{}` и проверяйте каждое поле независимо, записывая в карту только первое нарушенное правило поля — удобно через `switch` без выражения: ветки проверяются по порядку, срабатывает первая. В конце: `if len(errs) == 0 { return nil }` — пустую карту возвращать нельзя, это будет не-nil `error`.

## Подсказка 2

Длина в символах — `utf8.RuneCountInString(s)`, допустимые символы проверяйте в цикле `for _, c := range s` по рунам. Для email: `a, err := mail.ParseAddress(s)`, ошибка — если `err != nil || a.Address != s`. Ключ для тега — `fmt.Sprintf("tags[%d]", i)`.

## Подсказка 3

`Error()`: соберите ключи в срез, `sort.Strings`, затем `strings.Join` пар `k + ": " + fe[k]` через `"; "`. Для тегов в `Normalize` — новый срез и `seen := map[string]bool{}`: пропускайте пустые и уже встреченные. `Status`:

```go
var fe FieldErrors
if errors.As(err, &fe) {
	return http.StatusUnprocessableEntity
}
```
