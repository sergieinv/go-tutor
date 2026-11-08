// Одна и та же функция в двух редакциях: «как написали» и
// «после ревью». Обе находят пользователя в хранилище, но
// ведут себя по-разному, когда что-то идёт не так.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
)

var ErrNotFound = errors.New("not found")

// Хранилище уважает контекст: отменённый запрос не делает
// работу. В сервисе на его месте — `QueryRowContext`.
type memStore struct{ names map[int]string }

func (s *memStore) name(ctx context.Context, id int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	n, ok := s.names[id]
	if !ok {
		return "", ErrNotFound
	}
	return n, nil
}

// Логгер без времени — чтобы вывод примера был стабильным.
var logger = log.New(os.Stdout, "  лог: ", 0)

// До ревью: `Get` в имени, конкретный тип в параметре,
// свой `context.Background()` вместо контекста вызова,
// лог и возврат одновременно, `%v` вместо `%w`, ошибка
// с заглавной буквы и точкой.
func GetUserName(s *memStore, id int) (string, error) {
	n, err := s.name(context.Background(), id)
	if err != nil {
		logger.Printf("ошибка получения пользователя %d: %v", id, err)
		return "", fmt.Errorf("Failed to get user %d: %v.", id, err)
	}
	return n, nil
}

// После ревью: `ctx` первым параметром, интерфейс из одного
// метода объявлен здесь, у потребителя, ошибка обёрнута
// `%w` с контекстом, а решать, логировать ли, — вызывающему.
type nameSource interface {
	name(ctx context.Context, id int) (string, error)
}

func userName(ctx context.Context, src nameSource, id int) (string, error) {
	n, err := src.name(ctx, id)
	if err != nil {
		return "", fmt.Errorf("user %d: %w", id, err)
	}
	return n, nil
}

func main() {
	s := &memStore{names: map[int]string{1: "alice"}}
	ctx := context.Background()

	// Ошибка «не найден». Старая версия пишет в лог сама —
	// и вызывающий, которому тоже надо залогировать, получит
	// две записи об одном событии. А `%v` превратил причину
	// в текст: `errors.Is` её больше не видит.
	_, err := GetUserName(s, 2)
	fmt.Println("до:   ", err, errors.Is(err, ErrNotFound))
	_, err = userName(ctx, s, 2)
	fmt.Println("после:", err, errors.Is(err, ErrNotFound))

	// Клиент ушёл, контекст отменён. Старая версия этого не
	// знает и делает работу зря; новая останавливается, и
	// вызывающий отличает отмену от настоящей ошибки.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	n, err := userName(canceled, s, 1)
	fmt.Printf("после: %q %v %v\n", n, err, errors.Is(err, context.Canceled))
	n, err = GetUserName(s, 1)
	fmt.Printf("до:    %q %v (отмену не заметила)\n", n, err)
}
