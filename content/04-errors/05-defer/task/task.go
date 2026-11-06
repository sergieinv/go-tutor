package task

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
	// TODO: реализуйте функцию
	return nil
}
