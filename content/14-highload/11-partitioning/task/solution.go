package task

import (
	"errors"
	"fmt"
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

// validName — идентификатор без кавычек: строчные латинские буквы,
// цифры и «_», не с цифры, от 1 до max символов. Имена таблиц в
// DDL не передаются плейсхолдерами, поэтому — только белый список.
func validName(s string, max int) bool {
	if len(s) < 1 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Monthly возвращает месячные партиции, покрывающие [from, to).
func Monthly(parent string, from, to time.Time) ([]Partition, error) {
	if !validName(parent, 55) {
		return nil, fmt.Errorf("%w: %q", ErrBadName, parent)
	}
	if !from.Before(to) {
		return nil, ErrBadRange
	}
	var parts []Partition
	for m := monthStart(from); m.Before(to); m = m.AddDate(0, 1, 0) {
		if m.Year() < 1000 || m.Year() > 9999 {
			return nil, ErrBadRange
		}
		parts = append(parts, Partition{
			Parent: parent,
			Name:   fmt.Sprintf("%s_%04d_%02d", parent, m.Year(), int(m.Month())),
			From:   m,
			To:     m.AddDate(0, 1, 0),
		})
	}
	return parts, nil
}

// lit — литерал timestamptz с явным смещением: без него граница
// зависела бы от часового пояса сессии.
func lit(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05") + "+00"
}

func (p Partition) check() error {
	if !validName(p.Parent, 63) || !validName(p.Name, 63) {
		return fmt.Errorf("%w: %q, %q", ErrBadName, p.Parent, p.Name)
	}
	if !p.From.Before(p.To) {
		return ErrBadRange
	}
	return nil
}

// CreateSQL — DDL создания партиции (повторный запуск не ошибка).
func (p Partition) CreateSQL() (string, error) {
	if err := p.check(); err != nil {
		return "", err
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
		p.Name, p.Parent, lit(p.From), lit(p.To)), nil
}

// DetachSQL — DDL отсоединения партиции от родителя.
func (p Partition) DetachSQL() (string, error) {
	if err := p.check(); err != nil {
		return "", err
	}
	return fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s", p.Parent, p.Name), nil
}

// Locate находит партицию, в которую попадёт строка со временем t.
func Locate(parts []Partition, t time.Time) (Partition, bool) {
	for _, p := range parts {
		if !t.Before(p.From) && t.Before(p.To) {
			return p, true
		}
	}
	return Partition{}, false
}

// Expired — партиции целиком старше текущего месяца и keep
// предыдущих.
func Expired(parts []Partition, now time.Time, keep int) []Partition {
	keep = max(keep, 0)
	cutoff := monthStart(now).AddDate(0, -keep, 0)
	var out []Partition
	for _, p := range parts {
		if !p.To.After(cutoff) {
			out = append(out, p)
		}
	}
	return out
}
