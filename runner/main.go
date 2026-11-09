// runner — сервис исполнения кода учебника.
//
//	runner             HTTP-сервер (по умолчанию)
//	runner launch ...  внутренняя подкоманда: rlimit'ы и exec программы
//	runner killall     внутренняя подкоманда: убить все процессы своего uid
//
// Окружение:
//
//	RUNNER_MODE     local | server (по умолчанию local)
//	RUNNER_ADDR     адрес HTTP, по умолчанию :8081
//	RUNNER_CONTENT  каталог контента, по умолчанию ./content
//	RUNNER_SLOTS    число параллельных запусков, по умолчанию 4
//	RUNNER_UID_BASE первый uid слотов, по умолчанию 20000
//	RUNNER_GOVULNCHECK путь к govulncheck; пусто — ищется в PATH, нет — /api/vulncheck выключен
//	RUNNER_VULNDB   база уязвимостей для govulncheck -db, например file:///opt/vulndb
//	RUNNER_BUF      путь к buf (плагины protoc-gen-* рядом); пусто — ищется в PATH, нет — примеры с .proto не собираются
//	RUNNER_LAB_POSTGRES DSN администратора PostgreSQL профиля labs (runner.labs: [postgres])
//	RUNNER_LAB_MYSQL    DSN администратора MySQL профиля labs (runner.labs: [mysql])
//	RUNNER_LAB_KAFKA    брокеры Kafka профиля labs через запятую (runner.labs: [kafka])
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	osexec "os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"gotutor/runner/api"
	"gotutor/runner/sandbox"
)

func main() {
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "launch":
			err = sandbox.Launch(os.Args[2:])
			// Launch возвращается только при ошибке: иначе процесс заменён программой.
			fmt.Fprintln(os.Stderr, "runner launch:", err)
			os.Exit(126)
		case "killall":
			err = sandbox.KillAll()
		default:
			err = fmt.Errorf("неизвестная подкоманда %q", os.Args[1])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "runner:", err)
			os.Exit(1)
		}
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := serve(log); err != nil {
		log.Error("runner stopped", "err", err)
		os.Exit(1)
	}
}

func serve(log *slog.Logger) error {
	mode := env("RUNNER_MODE", "local")
	slots, err := strconv.Atoi(env("RUNNER_SLOTS", "4"))
	if err != nil || slots < 1 {
		return fmt.Errorf("RUNNER_SLOTS: нужно положительное число")
	}
	uidBase, err := strconv.Atoi(env("RUNNER_UID_BASE", "20000"))
	if err != nil {
		return fmt.Errorf("RUNNER_UID_BASE: %w", err)
	}

	cfg := sandbox.DefaultConfig()
	cfg.Slots = slots
	cfg.BuildEnv = []string{"GOPROXY=off"} // песочница без сети: модули только из прогретого кеша
	cfg.Govulncheck = os.Getenv("RUNNER_GOVULNCHECK")
	if cfg.Govulncheck == "" {
		cfg.Govulncheck, _ = osexec.LookPath("govulncheck")
	}
	cfg.VulnDB = os.Getenv("RUNNER_VULNDB")
	cfg.Buf = os.Getenv("RUNNER_BUF")
	if cfg.Buf == "" {
		cfg.Buf, _ = osexec.LookPath("buf")
	}
	cfg.Labs = map[string]string{}
	for name, key := range map[string]string{sandbox.LabPostgres: "RUNNER_LAB_POSTGRES", sandbox.LabMySQL: "RUNNER_LAB_MYSQL", sandbox.LabKafka: "RUNNER_LAB_KAFKA"} {
		if dsn := os.Getenv(key); dsn != "" {
			cfg.Labs[name] = dsn
		}
	}
	isolated := runtime.GOOS == "linux" && os.Geteuid() == 0
	switch mode {
	case "local":
		// В контейнере runner работает под root, и изоляция по uid включается сама.
		// При запуске прямо на машине разработчика — только таймауты и лимит вывода.
		if !isolated {
			log.Warn("runner без изоляции по uid: нужен Linux и root (запустите через docker compose)")
		}
	case "server":
		if !isolated {
			return errors.New("RUNNER_MODE=server требует Linux и root внутри контейнера (gVisor)")
		}
	default:
		return fmt.Errorf("RUNNER_MODE: неизвестный режим %q", mode)
	}
	if isolated {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		cfg.Launcher = self
		for i := range slots {
			cfg.UIDs = append(cfg.UIDs, uidBase+i)
		}
		// Временные каталоги должны быть доступны uid'ам слотов.
		cfg.WorkRoot = env("RUNNER_WORKDIR", "/tmp/runner")
		if err := os.MkdirAll(cfg.WorkRoot, 0o711); err != nil {
			return err
		}
	} else if runtime.GOOS == "linux" {
		self, _ := os.Executable()
		cfg.Launcher = self // rlimit'ы без смены uid
	}

	exec, err := sandbox.New(cfg)
	if err != nil {
		return err
	}
	lessons, err := api.LoadLessons(env("RUNNER_CONTENT", "content"))
	if err != nil {
		return fmt.Errorf("контент: %w", err)
	}

	srv := &http.Server{
		Addr:              env("RUNNER_ADDR", ":8081"),
		Handler:           api.NewServer(exec, lessons, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("runner started", "addr", srv.Addr, "mode", mode, "slots", slots, "isolated", isolated, "lessons", len(lessons), "govulncheck", cfg.Govulncheck != "", "vulndb", cfg.VulnDB)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
