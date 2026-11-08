package task

import (
	"bytes"
	"html"
	"mime"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Теги, которые есть в разметке страницы. Всё остальное — чужая
// разметка, которую браузер выполнит.
var tagRe = regexp.MustCompile(`(?i)<[a-z!/][^>]*>`)

var allowedTags = map[string]bool{
	"<!doctype html>": true, `<html lang="ru">`: true, "<head>": true, `<meta charset="utf-8">`: true,
	"<title>": true, "</title>": true, "</head>": true, "<body>": true, "<h1>": true, "</h1>": true,
	`<div class="bio">`: true, "</div>": true, "<b>": true, "</b>": true, "<br>": true,
	"</a>": true, "<script>": true, "</script>": true, "</body>": true, "</html>": true,
}

var linkTag = regexp.MustCompile(`^<a href="[^"<>]*" rel="nofollow ugc">$`)

// foreignTags возвращает теги страницы, которых нет в шаблоне.
func foreignTags(page string) []string {
	var bad []string
	for _, tag := range tagRe.FindAllString(page, -1) {
		if !allowedTags[tag] && !linkTag.MatchString(tag) {
			bad = append(bad, tag)
		}
	}
	return bad
}

func TestRenderSpecialChars(t *testing.T) {
	p := Profile{Name: `Tom & "Jerry" 'O'`, Bio: "a & b", Tags: []string{`"quoted"`, `back\slash`}}
	page := render(t, p)
	if !strings.Contains(page, "<h1>Tom &amp; &#34;Jerry&#34; &#39;O&#39;</h1>") {
		t.Errorf("имя в <h1> экранировано неверно:\n%s", page)
	}
	got := scriptJSON(t, page)
	want := map[string]any{"name": p.Name, "tags": []any{`"quoted"`, `back\slash`}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("profile = %v, want %v", got, want)
	}
	if strings.Contains(page, "<a href") {
		t.Errorf("пустой Website — ссылки быть не должно:\n%s", page)
	}
}

func TestRenderNilTags(t *testing.T) {
	got := scriptJSON(t, render(t, Profile{Name: "Ян"}))
	if !reflect.DeepEqual(got, map[string]any{"name": "Ян", "tags": nil}) {
		t.Errorf("profile = %v, want name Ян и tags null", got)
	}
}

func TestRenderHTTPLinks(t *testing.T) {
	for _, site := range []string{"http://example.com/", "https://example.com/a?b=c", "/users/42"} {
		page := render(t, Profile{Name: "x", Website: site})
		want := `<a href="` + html.EscapeString(site) + `" rel="nofollow ugc">`
		if !strings.Contains(page, want) {
			t.Errorf("Website %q: нет %s\n%s", site, want, page)
		}
	}
}

func TestExploitBioMarkup(t *testing.T) {
	if !strings.Contains(render(t, anna()), "<b>Go</b>") {
		t.Fatalf("разметка **жирный** не работает")
	}
	for _, bio := range []string{
		"<img src=x onerror=alert(1)>",
		"**<script>alert(1)</script>**",
		"<a href=javascript:alert(1)>клик</a>",
		"</div><svg onload=alert(1)>",
	} {
		page := render(t, Profile{Name: "x", Bio: bio})
		if bad := foreignTags(page); len(bad) > 0 {
			t.Errorf("Bio %q добавила в страницу теги %q", bio, bad)
		}
		if n := strings.Count(strings.ToLower(page), "<script"); n != 1 {
			t.Errorf("Bio %q: в странице %d тегов <script>, want 1", bio, n)
		}
	}
}

// schemeOf возвращает схему так, как её поймёт браузер: без
// пробелов по краям, табуляций и переводов строк, в нижнем регистре.
func schemeOf(href string) string {
	s := html.UnescapeString(href)
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, ":/?#"); i >= 0 && s[i] == ':' {
		return s[:i]
	}
	return ""
}

var hrefRe = regexp.MustCompile(`(?i)href\s*=\s*"([^"]*)"`)

func TestExploitWebsiteScheme(t *testing.T) {
	if !strings.Contains(render(t, anna()), `href="https://anna.dev/`) {
		t.Fatalf("обычная ссылка не работает")
	}
	for _, site := range []string{
		"javascript:alert(1)",
		"JaVaScRiPt:alert(document.cookie)",
		"  javascript:alert(1)",
		"java\tscript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
	} {
		page := render(t, Profile{Name: "x", Website: site})
		for _, m := range hrefRe.FindAllStringSubmatch(page, -1) {
			switch schemeOf(m[1]) {
			case "javascript", "data", "vbscript":
				t.Errorf("Website %q дал ссылку href=%q", site, m[1])
			}
		}
	}
}

func TestExploitScriptBreakout(t *testing.T) {
	scriptJSON(t, render(t, anna()))
	for _, p := range []Profile{
		{Name: "</script><script>alert(1)</script>"},
		{Name: "x", Tags: []string{"</SCRIPT><img src=x onerror=alert(1)>"}},
		{Name: "<!--<script>"},
	} {
		page := render(t, p)
		lower := strings.ToLower(page)
		if n, m := strings.Count(lower, "<script"), strings.Count(lower, "</script"); n != 1 || m != 1 {
			t.Errorf("Profile %+v: в странице %d открывающих и %d закрывающих script — данные вышли из скрипта", p, n, m)
			continue
		}
		if strings.Contains(lower, "<!--") {
			t.Errorf("Profile %+v: в странице комментарий <!--, он меняет разбор <script>", p)
		}
	}
}

func TestExploitAvatarSniff(t *testing.T) {
	rec := httptest.NewRecorder()
	AvatarHandler(png).ServeHTTP(rec, httptest.NewRequest("GET", "/avatar", nil))
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("PNG отдаётся не как image/png")
	}
	safe := map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "application/octet-stream": true}
	for _, data := range [][]byte{
		[]byte("<html><script>alert(1)</script></html>"),
		[]byte("<!DOCTYPE html><body onload=alert(1)>"),
		[]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`),
		[]byte("GIF89a<script>alert(1)</script>"), // настоящая сигнатура GIF
	} {
		rec := httptest.NewRecorder()
		AvatarHandler(data).ServeHTTP(rec, httptest.NewRequest("GET", "/avatar", nil))
		if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), data) {
			continue // отказ отдавать такой файл — тоже защита
		}
		ct := rec.Header().Get("Content-Type")
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || !safe[mt] {
			t.Errorf("загрузка %q отдана с Content-Type %q — браузер её отрисует", data, ct)
		}
		if v := rec.Header().Get("X-Content-Type-Options"); v != "nosniff" {
			t.Errorf("загрузка %q отдана без X-Content-Type-Options: nosniff — браузер вправе угадать тип", data)
		}
	}
}
