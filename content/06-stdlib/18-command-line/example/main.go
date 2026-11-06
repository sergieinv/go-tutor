// Аргументы командной строки лежат в `os.Args`, а разбирает
// их пакет `flag`. Здесь у программы аргументов нет, поэтому
// разбираем заданные в коде срезы — как сделала бы `main`
// с `os.Args[1:]`.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Свой тип флага реализует `flag.Value`: `String` и `Set`.
// `Set` вызывается на каждое вхождение — так флаг можно
// повторять: `-tag a -tag b`.
type tags []string

func (t *tags) String() string     { return strings.Join(*t, ",") }
func (t *tags) Set(v string) error { *t = append(*t, v); return nil }

// `FlagSet` — независимый набор флагов. `flag.String` и
// другие функции пакета работают с глобальным набором
// `flag.CommandLine`, но для подкоманд и тестов нужен свой.
func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stdout) // по умолчанию сообщения идут в stderr
	port := fs.Int("port", 8080, "порт HTTP-сервера")
	host := fs.String("host", "localhost", "адрес")
	verbose := fs.Bool("v", false, "подробный лог")
	timeout := fs.Duration("timeout", 30*time.Second, "таймаут запроса")
	var t tags
	fs.Var(&t, "tag", "метка (можно повторять)")

	// `ContinueOnError`: `Parse` возвращает ошибку, а не
	// завершает процесс. `-h` даёт `flag.ErrHelp`.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// После флагов — позиционные аргументы: `fs.Args()`.
	fmt.Printf("serve %s:%d v=%v timeout=%v tags=%v files=%q\n",
		*host, *port, *verbose, *timeout, t, fs.Args())
	return nil
}

func migrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // сообщения об ошибках печатаем сами
	steps := fs.Int("steps", 1, "сколько миграций применить")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Println("migrate steps =", *steps)
	return nil
}

// Подкоманды — это просто первый позиционный аргумент
// и отдельный `FlagSet` для каждой ветки.
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("нужна команда: serve или migrate")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "migrate":
		return migrate(args[1:])
	}
	return fmt.Errorf("неизвестная команда %q", args[0])
}

func main() {
	fmt.Println("os.Args[1:] пуст:", len(os.Args[1:]) == 0)

	for _, line := range []string{
		"serve -port 9000 --host=0.0.0.0 -v -tag api -tag v2 a.txt b.txt",
		"serve -timeout 1m30s -- -not-a-flag",
		"serve x.txt -v", // разбор флагов кончается на первом позиционном
		"migrate -steps=abc",
		"deploy",
		"serve -h",
	} {
		fmt.Println("$ app", line)
		if err := run(strings.Fields(line)); err != nil {
			fmt.Println("ошибка:", err)
		}
	}
}
