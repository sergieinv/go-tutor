package task

import (
	"errors"
	"time"
)

var (
	ErrBadName  = errors.New("bad name")
	ErrBadRange = errors.New("bad range")
)

// Partition — месячная партиция таблицы Parent: строки с
// From <= created_at < To.
type Partition struct {
	Parent string
	Name   string
	From   time.Time
	To     time.Time
}

// Monthly возвращает месячные партиции, покрывающие [from, to).
func Monthly(parent string, from, to time.Time) ([]Partition, error) {
	// TODO
	return nil, nil
}

// CreateSQL — DDL создания партиции (повторный запуск не ошибка).
func (p Partition) CreateSQL() (string, error) {
	// TODO
	return "", nil
}

// DetachSQL — DDL отсоединения партиции от родителя.
func (p Partition) DetachSQL() (string, error) {
	// TODO
	return "", nil
}

// Locate находит партицию, в которую попадёт строка со временем t.
func Locate(parts []Partition, t time.Time) (Partition, bool) {
	// TODO
	return Partition{}, false
}

// Expired — партиции целиком старше текущего месяца и keep
// предыдущих.
func Expired(parts []Partition, now time.Time, keep int) []Partition {
	// TODO
	return nil
}
