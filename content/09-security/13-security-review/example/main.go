// Ревью безопасности — это чек-лист атак, а не чтение
// кода «на глаз». Здесь чек-лист исполняемый: каждая
// атака — функция, которая бьёт по хендлеру и говорит,
// удалась ли она. Прогоняем её по уязвимой и исправленной
// версиям и печатаем таблицу — так же устроены эксплойты
// в задаче этого урока.

package main

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	texttemplate "text/template"
)

// Версия сервиса: набор хендлеров, которые проверяем.
type version struct {
	search   http.Handler // GET /search?q=… — страница результатов
	download http.Handler // GET /files?name=… — вложения из каталога
	order    http.Handler // GET /orders?id=… — заказ текущего пользователя
	upload   http.Handler // POST /upload — тело запроса
}

// Атака: что отправить и как понять, что она удалась.
type attack struct {
	name string
	run  func(v version) (pwned bool)
}

func call(h http.Handler, method, target string, body io.Reader, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("X-User", user) // аутентификацию сделал middleware
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var attacks = []attack{
	{"XSS в параметре поиска", func(v version) bool {
		rec := call(v.search, "GET", "/search?q=%3Cscript%3Ealert(1)%3C/script%3E", nil, "bob")
		return strings.Contains(rec.Body.String(), "<script>")
	}},
	{"path traversal ../", func(v version) bool {
		rec := call(v.download, "GET", "/files?name=../secret.env", nil, "bob")
		return strings.Contains(rec.Body.String(), "hunter2")
	}},
	{"чужой заказ (IDOR)", func(v version) bool {
		rec := call(v.order, "GET", "/orders?id=1", nil, "bob") // заказ 1 — у alice
		return rec.Code == http.StatusOK
	}},
	{"детали ошибки в ответе", func(v version) bool {
		rec := call(v.order, "GET", "/orders?id=x", nil, "bob")
		return strings.Contains(rec.Body.String(), "orders.db")
	}},
	{"тело 10 МиБ без лимита", func(v version) bool {
		rec := call(v.upload, "POST", "/upload", strings.NewReader(strings.Repeat("A", 10<<20)), "bob")
		return rec.Code == http.StatusOK
	}},
}

// Каталог вложений; рядом с ним, снаружи, — secret.env.
var filesDir string

// Хранилище заказов: id → владелец.
var orders = map[string]string{"1": "alice", "2": "bob"}

func loadOrder(id string) (owner string, err error) {
	owner, ok := orders[id]
	if !ok {
		return "", fmt.Errorf("orders.db: SELECT owner FROM orders WHERE id = %q: no rows", id)
	}
	return owner, nil
}

// Один и тот же шаблон двумя пакетами: `html/template`
// экранирует значение по контексту, `text/template` — нет.
var (
	searchText = texttemplate.Must(texttemplate.New("").Parse(`<p>Результаты для {{.}}</p>`))
	searchHTML = template.Must(template.New("").Parse(`<p>Результаты для {{.}}</p>`))
)

// УЯЗВИМО: `text/template` не экранирует HTML, `filepath.Join`
// пропускает `..`, владелец заказа не проверяется, текст
// ошибки уходит клиенту, тело читается без лимита.
var unsafe = version{
	search: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchText.Execute(w, r.URL.Query().Get("q"))
	}),
	download: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(filepath.Join(filesDir, r.URL.Query().Get("name")))
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Write(data)
	}),
	order: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := loadOrder(r.URL.Query().Get("id")); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		fmt.Fprintln(w, "заказ")
	}),
	upload: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
	}),
}

// Исправленная версия: те же хендлеры, по паре строк
// защиты в каждом. Чужой заказ неотличим от несуществующего.
var safe = version{
	search: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchHTML.Execute(w, r.URL.Query().Get("q"))
	}),
	download: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		root, err := os.OpenRoot(filesDir)
		if err == nil {
			defer root.Close()
			var data []byte
			if data, err = root.ReadFile(r.URL.Query().Get("name")); err == nil {
				w.Write(data)
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	}),
	order: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, err := loadOrder(r.URL.Query().Get("id"))
		if err != nil || owner != r.Header.Get("X-User") {
			http.Error(w, "not found", http.StatusNotFound) // чужой = несуществующий
			return
		}
		fmt.Fprintln(w, "заказ")
	}),
	upload: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		}
	}),
}

func main() {
	tmp, err := os.MkdirTemp("", "review")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	filesDir = filepath.Join(tmp, "files")
	os.Mkdir(filesDir, 0o755)
	os.WriteFile(filepath.Join(tmp, "secret.env"), []byte("DB_PASSWORD=hunter2"), 0o600)

	verdict := map[bool]string{true: "уязвим", false: "защищён"}
	fmt.Printf("%-26s %-9s %s\n", "атака", "unsafe", "safe")
	for _, a := range attacks {
		fmt.Printf("%-26s %-9s %s\n", a.name, verdict[a.run(unsafe)], verdict[a.run(safe)])
	}
}
