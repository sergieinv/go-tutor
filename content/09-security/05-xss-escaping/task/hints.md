## Подсказка 1

Шаблон — `html/template`, он экранирует всё сам. Значит, дыры там, где коду сказали «не экранируй»: найдите каждое приведение к `template.HTML`, `template.URL`, `template.JS` и спросите себя, может ли в этой строке оказаться ввод пользователя. И отдельно — посмотрите, какой `Content-Type` получит ответ `AvatarHandler`, если пользователь загрузил HTML вместо картинки.

## Подсказка 2

`FormatBio`: сначала `template.HTMLEscapeString` для всей строки, потом замены `**…**` и `\n` — свои теги добавляются уже в безопасный текст. `Website`: обычная `string` в `href` пройдёт через фильтр схем (`javascript:` станет `#ZgotmplZ`), а `&` превратится в `&amp;` — так и должно быть в HTML-атрибуте, ссылка не сломается. JSON: `SetEscapeHTML(false)` пропускает `</script>` в текст скрипта; проще всего отдать в поле `JSON` саму структуру `p` (тип `any`) — в `<script>` шаблон закодирует её в JSON и экранирует `<`.

## Подсказка 3

Без `Content-Type` `net/http` вызывает `http.DetectContentType` и честно говорит браузеру `text/html`. Определите тип сами и пропускайте только картинки:

```go
ct := http.DetectContentType(data)
switch ct {
case "image/png", "image/jpeg", "image/gif", "image/webp":
default:
	ct = "application/octet-stream"
}
w.Header().Set("Content-Type", ct)
w.Header().Set("X-Content-Type-Options", "nosniff")
```

`nosniff` обязателен: без него браузер вправе сам угадывать тип по содержимому.
