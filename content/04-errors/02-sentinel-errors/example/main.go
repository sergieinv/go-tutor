// Sentinel-ошибка — заранее объявленное значение,
// с которым вызывающий код сравнивает полученную
// ошибку. Стандартная библиотека полна таких:
// `io.EOF`, `sql.ErrNoRows`, `os.ErrNotExist`.

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Свои sentinel-ошибки объявляют переменными пакета.
// Имя начинается с `Err`, текст — с маленькой буквы
// и без точки в конце: его будут склеивать с контекстом.
var ErrNotFound = errors.New("not found")

var users = map[int]string{1: "alice", 2: "bob"}

func findUser(id int) (string, error) {
	name, ok := users[id]
	if !ok {
		return "", ErrNotFound
	}
	return name, nil
}

func main() {

	// `errors.Is` отвечает на вопрос «это та самая
	// ошибка?». «Не нашли» — ожидаемый исход, а не авария:
	// обрабатываем его отдельно от прочих ошибок.
	for _, id := range []int{1, 3} {
		name, err := findUser(id)
		switch {
		case errors.Is(err, ErrNotFound):
			fmt.Println(id, "— нет такого пользователя")
		case err != nil:
			fmt.Println(id, "— ошибка:", err)
		default:
			fmt.Println(id, "—", name)
		}
	}

	// `io.EOF` — самый известный sentinel. `Read` возвращает
	// его, когда данные кончились. Это сигнал выйти из цикла,
	// а не сбой. Обратите внимание: при `io.EOF` вызов
	// может вернуть и последние байты, поэтому сначала
	// обрабатываем `n`, а потом смотрим на ошибку.
	r := strings.NewReader("hello, go")
	buf := make([]byte, 4)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			fmt.Printf("прочитано %q\n", buf[:n])
		}
		if errors.Is(err, io.EOF) {
			fmt.Println("конец данных")
			break
		}
		if err != nil {
			fmt.Println("ошибка чтения:", err)
			break
		}
	}

	// Sentinel можно обернуть, добавив контекст.
	// `==` обёртку уже не узнает, а `errors.Is`
	// пройдёт по цепочке и найдёт исходное значение.
	err := fmt.Errorf("загрузка профиля 3: %w", ErrNotFound)
	fmt.Println(err)
	fmt.Println("==:", err == ErrNotFound)
	fmt.Println("errors.Is:", errors.Is(err, ErrNotFound))

	// Две ошибки с одинаковым текстом — разные значения.
	// Поэтому sentinel создают один раз и переиспользуют.
	fmt.Println("same text:", errors.New("not found") == ErrNotFound)
}
