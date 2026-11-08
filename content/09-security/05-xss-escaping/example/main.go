// XSS — это данные, которые браузер принял за код. Защита
// в Go — `html/template`: он разбирает разметку шаблона и
// экранирует каждое `{{…}}` по правилам того места, куда
// значение попадает.

package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	ttemplate "text/template"
)

// `text/template` ничего не знает об HTML: значение
// вставляется как есть.
const hello = "<p>Привет, {{.}}!</p>\n"

// Один шаблон — пять контекстов: текст, атрибут, параметр
// URL, начало URL и JavaScript.
const page = `<p>{{.Name}}</p>
<a title="{{.Name}}"
   href="/u?name={{.Name}}">профиль</a>
<a href="{{.Site}}">сайт</a>
<script>let user = {{.Name}};</script>
<div>{{.Bio}}</div>
`

type Profile struct {
	Name string
	Site string
	Bio  template.HTML // «доверенная» разметка — экранирования не будет
}

// УЯЗВИМО: строка пользователя объявлена безопасным HTML —
// `<img onerror=…>` в «биографии» выполнится у каждого читателя.
func bioUnsafe(s string) template.HTML {
	return template.HTML(strings.ReplaceAll(s, "\n", "<br>"))
}

// Сначала экранировать, потом добавлять свою разметку.
func bio(s string) template.HTML {
	s = template.HTMLEscapeString(s)
	return template.HTML(strings.ReplaceAll(s, "\n", "<br>"))
}

func main() {
	xss := "<script>alert(1)</script>"
	ttemplate.Must(ttemplate.New("t").Parse(hello)).Execute(os.Stdout, xss)
	template.Must(template.New("h").Parse(hello)).Execute(os.Stdout, xss)

	// Одно значение в разных контекстах — разные строки.
	// `javascript:` в начале URL заменён на `#ZgotmplZ`.
	t := template.Must(template.New("page").Parse(page))
	t.Execute(os.Stdout, Profile{
		Name: `Tom & "Jerry"`,
		Site: "javascript:alert(1)",
		Bio:  bio("<img src=x onerror=alert(1)>\nвторая строка"),
	})

	// `template.HTML` выключает экранирование — ровно там,
	// где оно нужнее всего.
	fmt.Println("unsafe:", bioUnsafe("<img src=x onerror=alert(1)>"))

	// Значение в `<script>` кодируется в JSON, причём `<`, `>`
	// и `&` превращаются в `\u003c`…, так что `</script>`
	// из данных не закроет тег.
	js := template.Must(template.New("js").Parse("<script>const p = {{.}};</script>\n"))
	js.Execute(os.Stdout, map[string]any{"name": "</script><b>", "admin": false})

	// Заголовки: тип ответа, запрет угадывать тип и CSP —
	// второй рубеж, если экранирование где-то пропустили.
	upload := []byte("<html><script>alert(1)</script>")
	for _, h := range []http.HandlerFunc{avatarUnsafe(upload), avatar(upload)} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest("GET", "/avatar", nil))
		fmt.Printf("avatar: %q nosniff=%q\n",
			rec.Header().Get("Content-Type"), rec.Header().Get("X-Content-Type-Options"))
	}
	rec := httptest.NewRecorder()
	securePage(rec, httptest.NewRequest("GET", "/", nil))
	fmt.Println("CSP:", rec.Header().Get("Content-Security-Policy"))
}

// УЯЗВИМО: без `Content-Type` сервер угадает тип по первым
// байтам — загруженный «аватар» с HTML станет `text/html`.
func avatarUnsafe(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}
}

// Тип — только из белого списка картинок, иначе — скачивание.
func avatar(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := http.DetectContentType(data)
		if ct != "image/png" && ct != "image/jpeg" && ct != "image/gif" {
			ct = "application/octet-stream"
			w.Header().Set("Content-Disposition", "attachment")
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(data)
	}
}

var greeting = template.Must(template.New("g").Parse(hello))

// CSP `script-src 'self'` разрешает скрипты только из файлов
// своего origin: встроенный `<script>` и `onclick="…"` браузер
// не выполнит, даже если XSS-нагрузка попала в страницу.
func securePage(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'")
	greeting.Execute(w, "Анна")
}
