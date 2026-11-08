// lessonlint проверяет структуру контента и прогоняет каждый урок:
// пример даёт ожидаемый вывод, эталон проходит тесты, шаблон задачи
// компилируется и не проходит тесты, ответы квизов совпадают с запуском.
//
//	go run ./tools/lessonlint [-update] [-run 05-concurrency/] [-j 4] [-govulncheck path] [-vulndb url]
//	    [-postgres dsn] [-mysql dsn] [-sqlc path] [content]
//
// Уроки с runner.labs запускаются на настоящих базах профиля labs
// (docker compose --profile labs up), если переданы DSN администратора:
//
//	-postgres 'postgres://runner:labs-runner@127.0.0.1:55432/postgres?sslmode=disable'
//	-mysql 'runner:labs-runner@tcp(127.0.0.1:53306)/'
//
// Без них пример такого урока только собирается.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	osexec "os/exec"
	"os/signal"
	"runtime"
	"slices"
	"strings"
	"sync"

	"gotutor/runner/sandbox"
)

func main() {
	update := flag.Bool("update", false, "перезаписать example/output.txt реальным выводом")
	only := flag.String("run", "", "проверять только уроки, чей id содержит подстроку")
	jobs := flag.Int("j", runtime.NumCPU(), "сколько уроков проверять параллельно")
	govulncheck := flag.String("govulncheck", "", "путь к govulncheck; по умолчанию ищется в PATH")
	vulndb := flag.String("vulndb", "", "база уязвимостей для govulncheck -db; по умолчанию https://vuln.go.dev")
	postgres := flag.String("postgres", "", "DSN администратора PostgreSQL профиля labs")
	mysql := flag.String("mysql", "", "DSN администратора MySQL профиля labs")
	sqlc := flag.String("sqlc", "", "путь к sqlc для сверки сгенерированного кода; по умолчанию ищется в PATH")
	flag.Parse()
	root := "content"
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	problems := map[string]*Problems{}
	modules, err := LoadModules(root, *update, problems)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lessonlint:", err)
		os.Exit(2)
	}

	var lessons []*Lesson
	for _, m := range modules {
		for _, l := range m.Lessons {
			if strings.Contains(l.ID(), *only) {
				lessons = append(lessons, l)
			}
		}
	}

	cfg := sandbox.DefaultConfig()
	cfg.Slots = max(*jobs, 1)
	cfg.Govulncheck, cfg.VulnDB = *govulncheck, *vulndb
	cfg.Labs = map[string]string{}
	if *postgres != "" {
		cfg.Labs[sandbox.LabPostgres] = *postgres
	}
	if *mysql != "" {
		cfg.Labs[sandbox.LabMySQL] = *mysql
	}
	if cfg.Govulncheck == "" {
		cfg.Govulncheck, _ = osexec.LookPath("govulncheck")
	}
	exec, err := sandbox.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lessonlint:", err)
		os.Exit(2)
	}
	checker := &Checker{Exec: exec, Update: *update, Sqlc: *sqlc}
	if checker.Sqlc == "" {
		checker.Sqlc, _ = osexec.LookPath("sqlc")
	}
	sem := make(chan struct{}, max(*jobs, 1))
	var wg sync.WaitGroup
	for _, l := range lessons {
		p := problems[l.ID()]
		if len(p.Errors) > 0 {
			continue // структура сломана — запускать нечего
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			checker.CheckLesson(ctx, l, p)
		})
	}
	wg.Wait()

	ids := make([]string, 0, len(problems))
	for id := range problems {
		if strings.Contains(id, *only) || !strings.Contains(id, "/") {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)

	failed := 0
	for _, id := range ids {
		p := problems[id]
		mark := "ok  "
		if len(p.Errors) > 0 {
			mark = "FAIL"
			failed++
		}
		fmt.Printf("%s %s\n", mark, id)
		for _, e := range p.Errors {
			fmt.Printf("     ✗ %s\n", e)
		}
		for _, w := range p.Warnings {
			fmt.Printf("     ! %s\n", w)
		}
	}
	fmt.Printf("\nуроков: %d, с ошибками: %d\n", len(lessons), failed)
	if failed > 0 {
		os.Exit(1)
	}
}
