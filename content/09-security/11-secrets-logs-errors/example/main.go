// Секреты утекают не через взлом, а через логи, `%v`
// и тексты ошибок. Защита в Go — типы и одна точка
// выхода: у секрета свои методы печати, у логгера —
// фильтр ключей, у хендлера — место, где ошибка
// делится на «подробно в лог» и «коротко клиенту».

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Секрет печатается как `[REDACTED]` любым способом:
// `Format` перехватывает все глаголы `fmt` (`%v`, `%+v`,
// `%#v`, `%s`, `%d`…), `LogValue` — для `slog`,
// `MarshalJSON` — для `encoding/json`. Настоящее значение
// достаётся только явным `Reveal`, его легко найти grep'ом.
type Secret string

const redacted = "[REDACTED]"

func (s Secret) Reveal() string              { return string(s) }
func (Secret) Format(f fmt.State, verb rune) { io.WriteString(f, redacted) }
func (Secret) LogValue() slog.Value          { return slog.StringValue(redacted) }
func (Secret) MarshalJSON() ([]byte, error)  { return json.Marshal(redacted) }

type Config struct {
	Addr       string
	DBPassword Secret
	APIToken   Secret
}

// Секреты — из окружения или из файла (`NAME_FILE` —
// так монтируют секреты Docker и Kubernetes), а не из
// кода: строка в репозитории остаётся в истории git навсегда.
func loadSecret(name string) (Secret, error) {
	if path, ok := os.LookupEnv(name + "_FILE"); ok {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("секрет %s: %w", name, err)
		}
		return Secret(strings.TrimSpace(string(b))), nil
	}
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return Secret(v), nil
	}
	return "", fmt.Errorf("секрет %s не задан", name) // имя — да, значение — никогда
}

// Второй рубеж — `ReplaceAttr`: значения под опасными
// ключами заменяются в любой группе, даже если кто-то
// положил пароль в лог обычной строкой. Время убираем,
// чтобы вывод примера не менялся.
func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch strings.ToLower(a.Key) {
			case "password", "token", "authorization", "cookie", "secret":
				return slog.String(a.Key, redacted)
			case slog.TimeKey:
				if len(groups) == 0 {
					return slog.Attr{}
				}
			}
			return a
		},
	}))
}

// Хендлеры возвращают ошибку, а отвечает на неё одна
// обёртка: подробности — в лог с id запроса, клиенту —
// общий текст и тот же id, по которому поддержка найдёт
// запись. Паника — тоже 500, без стека в ответе.
type appHandler func(w http.ResponseWriter, r *http.Request) error

var lastID atomic.Int64

func handle(log *slog.Logger, h appHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// В продакшне — crypto/rand.Text(); здесь счётчик ради вывода.
		id := fmt.Sprintf("req-%d", lastID.Add(1))
		fail := func() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "internal error", "request_id": id})
		}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				// В настоящем логе добавьте "stack", string(debug.Stack()).
				log.Error("panic", "request_id", id, "panic", fmt.Sprint(v))
				fail()
			}
		}()
		if err := h(w, r); err != nil {
			log.Error("request failed", "request_id", id, "path", r.URL.Path, "err", err)
			fail()
		}
	})
}

// Окружение и файл с секретом пример создаёт сам — в
// жизни их задаёт оркестратор, а в коде секретов нет.
func main() {
	dir, _ := os.MkdirTemp("", "secrets")
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "db_password"), []byte("hunter2\n"), 0o600)
	os.Setenv("DB_PASSWORD_FILE", filepath.Join(dir, "db_password"))
	os.Setenv("API_TOKEN", "tok-9f8e7d")

	var cfg Config
	cfg.Addr = "127.0.0.1:8080"
	cfg.DBPassword, _ = loadSecret("DB_PASSWORD")
	cfg.APIToken, _ = loadSecret("API_TOKEN")
	_, err := loadSecret("SMTP_PASSWORD")
	fmt.Println(err)
	fmt.Printf("%v\n%+v\n%#v\n%d\n", cfg, cfg, cfg, cfg.APIToken)
	fmt.Println("для подключения:", len(cfg.DBPassword.Reveal()), "символов")

	log := newLogger(os.Stdout)
	log.Info("config", "cfg", cfg, "api", cfg.APIToken)
	log.Info("login", "user", "bob", slog.Group("req", "Authorization", "Bearer abc"), "password", "qwerty")

	mux := http.NewServeMux()
	mux.Handle("GET /orders/{id}", handle(log, func(w http.ResponseWriter, r *http.Request) error {
		return fmt.Errorf("load order %s: %w", r.PathValue("id"),
			errors.New(`pq: password authentication failed for user "app"`))
	}))
	mux.Handle("GET /report", handle(log, func(w http.ResponseWriter, r *http.Request) error {
		var m map[string]int
		m["x"]++ // запись в nil-карту — паника
		return nil
	}))
	for _, path := range []string{"/orders/42", "/report"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		fmt.Print("клиент: ", rec.Code, " ", rec.Body.String())
	}
}
