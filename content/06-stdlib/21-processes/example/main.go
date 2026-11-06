// Пакет `os/exec` запускает внешние программы. `exec.Command`
// описывает запуск, а `Run`, `Output`, `CombinedOutput`
// выполняют его и ждут завершения.

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Пример перезапускает сам себя в роли дочернего процесса:
// режим передаётся через переменную окружения.
func child(mode string) {
	switch mode {
	case "upper": // stdin → stdout заглавными буквами
		data, _ := io.ReadAll(os.Stdin)
		fmt.Print(strings.ToUpper(string(data)))
	case "lines": // три строки с паузами
		for i := 1; i <= 3; i++ {
			fmt.Println("строка", i)
			time.Sleep(10 * time.Millisecond)
		}
	case "hang": // «зависший» процесс
		time.Sleep(time.Minute)
	}
}

func self(ctx context.Context, mode string) *exec.Cmd {
	exe, _ := os.Executable()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), "CHILD_MODE="+mode)
	return cmd
}

func main() {
	if mode := os.Getenv("CHILD_MODE"); mode != "" {
		child(mode)
		return
	}

	// Аргументы передаются программе как есть — без оболочки.
	// `$HOME` и `*` не раскрываются, пробел внутри аргумента
	// не делит его на два. Подстановка команд невозможна.
	out, _ := exec.Command("echo", "$HOME", "*", "a b").Output()
	fmt.Printf("echo: %s", out)

	// Нужна оболочка — вызывайте её явно. Ввод пользователя
	// в строку для `sh -c` не подставляйте.
	out, _ = exec.Command("/bin/sh", "-c", "echo $((6 * 7))").Output()
	fmt.Printf("sh: %s", out)

	// `CombinedOutput` собирает stdout и stderr вместе.
	out, _ = exec.Command("/bin/sh", "-c", "echo out; echo err >&2").CombinedOutput()
	fmt.Printf("combined: %q\n", out)

	// Ненулевой код выхода — ошибка `*exec.ExitError`.
	err := exec.Command("/bin/sh", "-c", "exit 3").Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		fmt.Println("код выхода:", exitErr.ExitCode())
	}

	// Программы нет в PATH — ошибка ещё до запуска.
	err = exec.Command("no-such-tool").Run()
	fmt.Println("не найдена:", errors.Is(err, exec.ErrNotFound))

	// stdin — любой `io.Reader`, stdout — любой `io.Writer`.
	cmd := self(context.Background(), "upper")
	cmd.Stdin = strings.NewReader("привет, процесс\n")
	var sb strings.Builder
	cmd.Stdout = &sb
	cmd.Run()
	fmt.Print("upper: ", sb.String())

	// `StdoutPipe` читает вывод по мере появления.
	// Сначала `Start`, потом чтение, в конце — `Wait`.
	cmd = self(context.Background(), "lines")
	pipe, _ := cmd.StdoutPipe()
	cmd.Start()
	sc := bufio.NewScanner(pipe)
	for sc.Scan() {
		fmt.Println("получено:", sc.Text())
	}
	cmd.Wait()

	// `CommandContext` убивает процесс, когда контекст
	// отменён или истёк таймаут. `Run` вернёт ошибку
	// «signal: killed», а причину покажет `ctx.Err()`.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = self(ctx, "hang").Run()
	fmt.Println("таймаут:", err, "| ctx:", ctx.Err())
}
