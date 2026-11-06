// Сигналы — способ ОС сказать процессу «остановись»
// (`SIGTERM` от Docker и systemd, `SIGINT` от Ctrl+C).
// Пакет `os/signal` превращает их в значения канала или
// в отмену контекста. Здесь сигналы посылаем сами себе.

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// Дочерние режимы: показывают, как завершается процесс.
func child(mode string) {
	defer fmt.Println("defer выполнен") // при os.Exit — нет
	switch mode {
	case "exit":
		fmt.Println("os.Exit(3)")
		os.Exit(3)
	case "fatal":
		log.SetOutput(os.Stdout)
		log.SetFlags(0)
		log.Fatal("log.Fatal") // печать + os.Exit(1)
	case "panic":
		panic("boom") // defer выполнится, код выхода 2
	case "return":
		fmt.Println("обычный return")
	}
}

func runChild(mode string) {
	exe, _ := os.Executable()
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "CHILD_MODE="+mode)
	out, _ := cmd.Output()
	fmt.Printf("  %-6s → код %d, вывод %q\n", mode, cmd.ProcessState.ExitCode(), out)
}

func main() {
	if mode := os.Getenv("CHILD_MODE"); mode != "" {
		child(mode)
		return
	}

	// `signal.Notify` перенаправляет сигнал в канал вместо
	// действия по умолчанию (для SIGUSR1 — завершение).
	// Канал буферизованный: отправка в него неблокирующая.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	syscall.Kill(os.Getpid(), syscall.SIGUSR1)
	fmt.Println("получен:", <-ch == syscall.SIGUSR1)
	signal.Stop(ch) // вернуть поведение по умолчанию

	// Graceful shutdown: `NotifyContext` отменяет контекст
	// по сигналу. Воркер замечает отмену, дописывает
	// текущую работу и сообщает, что закончил.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	done := make(chan string)
	go func() {
		<-ctx.Done()
		// ... дописываем текущую работу ...
		done <- "состояние сохранено"
	}()
	syscall.Kill(os.Getpid(), syscall.SIGTERM) // так делает `docker stop`
	<-ctx.Done()
	fmt.Println("main: остановка,", ctx.Err())

	// Ждём воркер, но не бесконечно: оркестратор после
	// SIGTERM даёт ограниченное время, а потом шлёт SIGKILL.
	select {
	case msg := <-done:
		fmt.Println("воркер:", msg)
	case <-time.After(5 * time.Second):
		fmt.Println("воркер не успел")
	}
	stop() // повторный SIGTERM снова завершит процесс сразу

	// Коды выхода показываем на дочерних процессах:
	// `os.Exit` завершает процесс немедленно, без `defer`,
	// а необработанная паника выполняет `defer` и даёт код 2.
	fmt.Println("дочерние процессы:")
	for _, mode := range []string{"return", "exit", "fatal", "panic"} {
		runChild(mode)
	}
}
