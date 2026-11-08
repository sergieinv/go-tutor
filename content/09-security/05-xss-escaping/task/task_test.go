package task

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func anna() Profile {
	return Profile{
		Name:    "Анна",
		Bio:     "**Go**-разработчик\nпишу про C#",
		Website: "https://anna.dev/?tab=posts&page=2",
		Tags:    []string{"go", "dotnet"},
	}
}

func render(t *testing.T, p Profile) string {
	t.Helper()
	var sb strings.Builder
	if err := RenderProfile(&sb, p); err != nil {
		t.Fatalf("RenderProfile: %v", err)
	}
	return sb.String()
}

// scriptJSON достаёт значение profile из <script> и разбирает его как JSON.
func scriptJSON(t *testing.T, page string) map[string]any {
	t.Helper()
	const start, end = "<script>const profile = ", ";</script>"
	i := strings.Index(page, start)
	j := strings.LastIndex(page, end)
	if i < 0 || j < i {
		t.Fatalf("нет <script>const profile = …;</script> в странице:\n%s", page)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(page[i+len(start):j]), &v); err != nil {
		t.Fatalf("profile в <script> — не JSON: %v\n%s", err, page[i:j])
	}
	return v
}

func TestFormatBio(t *testing.T) {
	tests := []struct{ in, want string }{
		{"**Go** и C#\nвторая строка", "<b>Go</b> и C#<br>вторая строка"},
		{"без разметки", "без разметки"},
		{"**раз** и **два**", "<b>раз</b> и <b>два</b>"},
		{"**не закрыто", "**не закрыто"},
	}
	for _, tt := range tests {
		if got := FormatBio(tt.in); string(got) != tt.want {
			t.Errorf("FormatBio(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRenderProfile(t *testing.T) {
	page := render(t, anna())
	for _, want := range []string{
		"<title>Анна</title>",
		"<h1>Анна</h1>",
		`<div class="bio"><b>Go</b>-разработчик<br>пишу про C#</div>`,
		`<a href="https://anna.dev/?tab=posts&amp;page=2" rel="nofollow ugc">сайт</a>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("в странице нет %s\n%s", want, page)
		}
	}
	got := scriptJSON(t, page)
	want := map[string]any{"name": "Анна", "tags": []any{"go", "dotnet"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("profile в <script> = %v, want %v", got, want)
	}
}

func TestProfileHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	ProfileHandler(anna()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<h1>Анна</h1>") {
		t.Fatalf("ответ %d:\n%s", rec.Code, rec.Body)
	}
	h := rec.Header()
	if ct := h.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if v := h.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
	}
}

// png — начало настоящего PNG-файла.
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01")

func TestAvatarHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	AvatarHandler(png).ServeHTTP(rec, httptest.NewRequest("GET", "/avatar", nil))
	if rec.Code != 200 || !reflect.DeepEqual(rec.Body.Bytes(), png) {
		t.Fatalf("ответ %d, тело %q", rec.Code, rec.Body.Bytes())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
}
