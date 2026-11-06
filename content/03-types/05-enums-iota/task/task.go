package task

// Perm — набор прав доступа: битовые флаги.
type Perm uint8

// TODO: объявите Read, Write, Exec через iota (значения 1, 2, 4).
const (
	Read  Perm = 0
	Write Perm = 0
	Exec  Perm = 0
)

// String возвращает права в виде "rwx", отсутствующий флаг — "-".
func (p Perm) String() string {
	// TODO: реализуйте метод
	return ""
}

// Has сообщает, установлены ли в p все флаги из q.
func (p Perm) Has(q Perm) bool {
	// TODO: реализуйте метод
	return false
}

// ParsePerm разбирает строку вида "r-x".
func ParsePerm(s string) (Perm, error) {
	// TODO: реализуйте функцию
	return 0, nil
}
