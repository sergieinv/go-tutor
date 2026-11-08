## Подсказка 1

Проверку списка удобно сделать при создании: `url.Parse`, схема, `u.Host != ""` и сравнение `o == u.Scheme+"://"+u.Host` разом отсекают путь, `/` в конце, запрос, фрагмент и `user@`; заглавные буквы — `o != strings.ToLower(o)`. Разрешённые origin сложите в `map[string]bool` — дальше «разрешён» значит `allowed[r.Header.Get("Origin")]`, и никаких `HasPrefix`/`HasSuffix`.

## Подсказка 2

Middleware: сначала `w.Header().Add("Vary", "Origin")`, потом ветка preflight — `r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""` — с ответом `204` или `403` и `return` без вызова `next`. Для остальных запросов заголовки ставятся только разрешённому origin, а `next` вызывается всегда: CORS не блокирует запросы, он разрешает читать ответы.

## Подсказка 3

Защиту от CSRF не пишите руками — правила из условия реализует `http.CrossOriginProtection`:

```go
cop := http.NewCrossOriginProtection()
for _, o := range origins {
	cop.AddTrustedOrigin(o)
}
return cors(cop.Handler(next)), nil
```

CORS снаружи — чтобы и ответ `403` нёс `Vary: Origin`, а preflight отвечался до проверки (метод `OPTIONS` она и так пропускает).
