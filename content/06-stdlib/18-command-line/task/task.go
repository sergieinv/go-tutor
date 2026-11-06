package task

import "errors"

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

// Parse разбирает аргументы без имени программы.
func Parse(args []string) (Command, error) {
	// TODO: реализуйте функцию
	return Command{}, nil
}
