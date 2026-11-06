// `log/slog` — структурное логирование в стандартной
// библиотеке: запись состоит из уровня, сообщения и
// атрибутов «ключ — значение», а формат вывода задаёт
// обработчик (`Handler`).

package main

import (
	"context"
	"log/slog"
	"os"
)

// `LogValuer` определяет, как значение попадёт в лог.
// Так прячут секреты и сокращают большие структуры.
type User struct {
	ID       int
	Email    string
	Password string
}

func (u User) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("id", u.ID),
		slog.String("email", u.Email),
	)
}

// `ReplaceAttr` правит каждый атрибут перед выводом.
// Здесь убираем время, чтобы вывод примера не менялся.
func noTime(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey && len(groups) == 0 {
		return slog.Attr{} // пустой атрибут не выводится
	}
	return a
}

func main() {
	// Уровень отсечения — через `LevelVar`: его можно менять
	// на лету, не пересоздавая логгер.
	var level slog.LevelVar // по умолчанию Info
	opts := &slog.HandlerOptions{Level: &level, ReplaceAttr: noTime}
	log := slog.New(slog.NewTextHandler(os.Stdout, opts))

	// Атрибуты — чередование ключей и значений или
	// типизированные `slog.Int`, `slog.String`… (без аллокаций).
	log.Info("сервер запущен", "port", 8080, slog.Bool("tls", true))
	log.Debug("не выведется: уровень ниже Info")
	level.Set(slog.LevelDebug)
	log.Debug("теперь выведется")

	// `With` возвращает логгер с атрибутами, которые
	// добавятся к каждой записи.
	reqLog := log.With("request_id", "r-42")
	reqLog.Warn("медленный запрос", "ms", 1250)

	// Группы вкладывают атрибуты под общий ключ:
	// в тексте — `http.method=GET`, в JSON — вложенный объект.
	reqLog.Info("ответ", slog.Group("http", "method", "GET", "status", 200))

	// `LogValue` вызывается при выводе — пароля в логе не будет.
	u := User{ID: 7, Email: "a@b.c", Password: "secret"}
	log.Error("вход не удался", "user", u)

	// Тот же код с другим обработчиком пишет JSON.
	// `WithGroup` кладёт все последующие атрибуты в группу.
	jlog := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: noTime}))
	jlog.WithGroup("db").Info("запрос", "table", "users", "rows", 3)
	jlog.Info("вход", "user", u)

	// Варианты с `Context` передают контекст обработчику —
	// он может достать оттуда trace id (см. задание).
	ctx := context.Background()
	jlog.InfoContext(ctx, "с контекстом")

	// `slog.SetDefault` делает логгер глобальным: им станут
	// пользоваться и `slog.Info`, и старый пакет `log`.
	slog.SetDefault(log)
	slog.Info("через пакетную функцию")
}
