package task

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Profile — публичный профиль пользователя. Все поля заполняет
// пользователь.
type Profile struct {
	Name    string   `json:"name"`
	Bio     string   `json:"-"`
	Website string   `json:"-"`
	Tags    []string `json:"tags"`
}

var bold = regexp.MustCompile(`\*\*(.+?)\*\*`)

// FormatBio превращает разметку биографии в HTML:
// **текст** — жирный, перевод строки — <br>.
func FormatBio(s string) template.HTML {
	// Сначала экранируем весь ввод, потом добавляем свою разметку:
	// в template.HTML не должно попасть ни одного символа пользователя
	// без экранирования.
	s = template.HTMLEscapeString(s)
	s = bold.ReplaceAllString(s, "<b>$1</b>")
	s = strings.ReplaceAll(s, "\n", "<br>")
	return template.HTML(s)
}

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="ru">
<head><meta charset="utf-8"><title>{{.Name}}</title></head>
<body>
<h1>{{.Name}}</h1>
<div class="bio">{{.Bio}}</div>
{{with .Website}}<a href="{{.}}" rel="nofollow ugc">сайт</a>{{end}}
<script>const profile = {{.JSON}};</script>
</body>
</html>
`))

type pageData struct {
	Name    string
	Bio     template.HTML
	Website string // обычная строка: шаблон отфильтрует javascript: и data:
	JSON    any    // значение в <script> шаблон сам закодирует в JSON
}

// RenderProfile пишет HTML-страницу профиля.
func RenderProfile(w io.Writer, p Profile) error {
	return pageTmpl.Execute(w, pageData{
		Name:    p.Name,
		Bio:     FormatBio(p.Bio),
		Website: p.Website,
		JSON:    p,
	})
}

// ProfileHandler отдаёт страницу профиля.
func ProfileHandler(p Profile) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		if err := RenderProfile(&buf, p); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(buf.Bytes())
	})
}

// AvatarHandler отдаёт загруженный пользователем аватар.
func AvatarHandler(data []byte) http.Handler {
	// Тип определяем сами и только из белого списка картинок.
	ct := http.DetectContentType(data)
	switch ct {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		ct = "application/octet-stream"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "public, max-age=3600")
		h.Set("Content-Type", ct)
		// Без nosniff браузер вправе угадать тип по содержимому.
		h.Set("X-Content-Type-Options", "nosniff")
		if ct == "application/octet-stream" {
			h.Set("Content-Disposition", "attachment")
		}
		w.Write(data)
	})
}
