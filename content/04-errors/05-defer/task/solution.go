package task

import "fmt"

// Resource — открытый ресурс: его используют и закрывают.
type Resource interface {
	Use() error
	Close() error
}

// Opener открывает ресурс по имени.
type Opener func(name string) (Resource, error)

// UseAll по очереди открывает, использует и закрывает ресурсы.
// Ошибка Use важнее ошибки Close. Возвращаемая ошибка
// начинается с "<name>: " и оборачивает исходную.
func UseAll(open Opener, names []string) error {
	for _, name := range names {
		// Отдельная функция: defer сработает в конце
		// каждой итерации, а не всего UseAll.
		if err := useOne(open, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func useOne(open Opener, name string) (err error) {
	r, err := open(name)
	if err != nil {
		return err
	}
	defer func() {
		// Ошибка Close заменяет результат, только если Use прошёл.
		if cerr := r.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return r.Use()
}
