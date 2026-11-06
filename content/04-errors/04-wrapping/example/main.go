// Каждый уровень программы добавляет к ошибке свой
// контекст, но исходная причина не должна теряться.
// Для этого ошибку оборачивают: `%w` в `fmt.Errorf`
// кладёт старую ошибку внутрь новой.

package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

// Свой тип ошибки тоже может быть обёрткой: достаточно
// метода `Unwrap() error`, возвращающего причину.
type QueryError struct {
	Query string
	Err   error
}

func (e *QueryError) Error() string { return e.Query + ": " + e.Err.Error() }
func (e *QueryError) Unwrap() error { return e.Err }

func findUser(id int) error {
	return &QueryError{Query: "SELECT users", Err: ErrNotFound}
}

func loadProfile(id int) error {
	if err := findUser(id); err != nil {
		return fmt.Errorf("load profile %d: %w", id, err)
	}
	return nil
}

func main() {

	// Текст обёрток склеивается через двоеточия —
	// получается читаемая цепочка от общего к частному.
	err := loadProfile(7)
	fmt.Println(err)

	// `errors.Unwrap` снимает один слой. Пройдём
	// цепочку вручную, чтобы увидеть все звенья.
	for e := err; e != nil; e = errors.Unwrap(e) {
		fmt.Printf("  %T\n", e)
	}

	// `errors.Is` и `errors.As` делают этот обход сами:
	// ищут в цепочке нужное значение или нужный тип.
	var qe *QueryError
	fmt.Println("is ErrNotFound:", errors.Is(err, ErrNotFound))
	fmt.Println("as QueryError:", errors.As(err, &qe), qe.Query)

	// `errors.Join` объединяет несколько ошибок в одну —
	// удобно для валидации, где важны все проблемы сразу.
	// `nil`-ошибки пропускаются, текст склеивается
	// через перевод строки.
	errName := errors.New("name: required")
	joined := errors.Join(errName, nil, fmt.Errorf("manager: %w", ErrNotFound))
	fmt.Println(joined)
	fmt.Println("joined is ErrNotFound:", errors.Is(joined, ErrNotFound))
	fmt.Println("join of nils:", errors.Join(nil, nil))

	// Несколько `%w` в одном `fmt.Errorf` тоже дают
	// ошибку с несколькими причинами. Такую ошибку
	// `errors.Unwrap` не разворачивает — у неё метод
	// `Unwrap() []error`, а не `Unwrap() error`.
	errTimeout := errors.New("timeout")
	both := fmt.Errorf("sync: %w, then %w", ErrNotFound, errTimeout)
	fmt.Println(both)
	fmt.Println("is timeout:", errors.Is(both, errTimeout))
	fmt.Println("unwrap:", errors.Unwrap(both))

	// `%v` вместо `%w` оставляет только текст. Так
	// скрывают детали реализации, когда вызывающий
	// не должен зависеть от внутренней ошибки.
	opaque := fmt.Errorf("load profile: %v", ErrNotFound)
	fmt.Println("opaque is ErrNotFound:", errors.Is(opaque, ErrNotFound))
}
