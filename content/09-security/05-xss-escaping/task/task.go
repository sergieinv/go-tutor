package task

import (
	"bytes"
	"encoding/json"
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
	Website template.URL
	JSON    template.JS
}

// RenderProfile пишет HTML-страницу профиля.
func RenderProfile(w io.Writer, p Profile) error {
	// Без HTML-экранирования: скрипту нужен текст как есть, а не \u003c.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return err
	}
	return pageTmpl.Execute(w, pageData{
		Name: p.Name,
		Bio:  FormatBio(p.Bio),
		// template.URL — чтобы шаблон не испортил & в параметрах ссылки.
		Website: template.URL(p.Website),
		JSON:    template.JS(strings.TrimSpace(buf.String())),
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(data) // тип ответа net/http определит по содержимому
	})
}
