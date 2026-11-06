package task

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Command — разобранная подкоманда.
type Command struct {
	Name     string   // "add" или "list"
	Priority int      // add: -p, по умолчанию 1, допустимо 1..5
	Tags     []string // add: -tag, флаг можно повторять
	Text     string   // add: позиционные аргументы через один пробел
	All      bool     // list: -all
	Limit    int      // list: -limit, по умолчанию 10, не меньше 1
}

var (
	ErrNoCommand      = errors.New("no command")
	ErrUnknownCommand = errors.New("unknown command")
)

// tagList — повторяемый флаг: Set вызывается на каждое вхождение.
type tagList []string

func (t *tagList) String() string     { return strings.Join(*t, ",") }
func (t *tagList) Set(v string) error { *t = append(*t, v); return nil }

// Parse разбирает аргументы без имени программы.
func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, ErrNoCommand
	}
	var (
		cmd Command
		err error
	)
	switch args[0] {
	case "add":
		cmd, err = parseAdd(args[1:])
	case "list":
		cmd, err = parseList(args[1:])
	default:
		err = fmt.Errorf("%w: %q", ErrUnknownCommand, args[0])
	}
	if err != nil {
		return Command{}, err
	}
	return cmd, nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // ни сообщений об ошибках, ни справки в stderr
	return fs
}

func parseAdd(args []string) (Command, error) {
	fs := newFlagSet("add")
	cmd := Command{Name: "add"}
	fs.IntVar(&cmd.Priority, "p", 1, "приоритет 1..5")
	var tags tagList
	fs.Var(&tags, "tag", "метка, можно повторять")
	if err := fs.Parse(args); err != nil {
		return Command{}, err
	}
	if cmd.Priority < 1 || cmd.Priority > 5 {
		return Command{}, fmt.Errorf("add: приоритет %d вне диапазона 1..5", cmd.Priority)
	}
	cmd.Tags = tags
	cmd.Text = strings.Join(fs.Args(), " ")
	if cmd.Text == "" {
		return Command{}, errors.New("add: нужен текст задачи")
	}
	return cmd, nil
}

func parseList(args []string) (Command, error) {
	fs := newFlagSet("list")
	cmd := Command{Name: "list"}
	fs.BoolVar(&cmd.All, "all", false, "показать и выполненные")
	fs.IntVar(&cmd.Limit, "limit", 10, "сколько задач показать")
	if err := fs.Parse(args); err != nil {
		return Command{}, err
	}
	if fs.NArg() > 0 {
		return Command{}, fmt.Errorf("list: лишние аргументы %q", fs.Args())
	}
	if cmd.Limit < 1 {
		return Command{}, fmt.Errorf("list: limit должен быть не меньше 1, получено %d", cmd.Limit)
	}
	return cmd, nil
}
