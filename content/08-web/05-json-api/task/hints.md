## Подсказка 1

Начните с двух хелперов ответа — дальше каждый хендлер пишется в несколько строк:

```go
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
```

`writeProblem(w, status, detail)` устроен так же, только с `application/problem+json` и `Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail}`. После каждого вызова `writeProblem` в хендлере — `return`.

## Подсказка 2

Порядок проверок `POST` повторяет условие. Медиатип — `mime.ParseMediaType` (он приводит тип к нижнему регистру и отделяет параметры). Чтобы `413` побеждал синтаксическую ошибку, сначала прочитайте всё тело: `io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))` — превышение даст `*http.MaxBytesError`. Затем `json.NewDecoder(bytes.NewReader(body))`, `DisallowUnknownFields()`, `Decode`, и второй `Decode(&struct{}{})` должен вернуть ровно `io.EOF`.

## Подсказка 3

`null` в структуру декодируется без ошибки и оставляет её нулевой. Декодируйте в указатель на структуру запроса — после `null` он останется `nil`:

```go
var in *struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}
// decode(&in) ...; if in == nil → 400
```

Длина имени — `utf8.RuneCountInString`. Email: `strings.Cut(email, "@")`, обе части непустые, во второй нет `@`, во всей строке нет пробельных символов (`strings.IndexFunc(email, unicode.IsSpace) < 0`). Проверка дубликата и выдача id — под одним захватом мьютекса, иначе два параллельных запроса с одним email оба пройдут проверку.
