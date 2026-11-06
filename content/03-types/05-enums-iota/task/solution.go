package task

import "fmt"

// Perm — набор прав доступа: битовые флаги.
type Perm uint8

const (
	Read Perm = 1 << iota
	Write
	Exec
)

// Порядок флагов и их буквы в строковом виде.
var permLetters = [...]struct {
	flag   Perm
	letter byte
}{{Read, 'r'}, {Write, 'w'}, {Exec, 'x'}}

// String возвращает права в виде "rwx", отсутствующий флаг — "-".
func (p Perm) String() string {
	b := []byte("---")
	for i, pl := range permLetters {
		if p&pl.flag != 0 {
			b[i] = pl.letter
		}
	}
	return string(b)
}

// Has сообщает, установлены ли в p все флаги из q.
func (p Perm) Has(q Perm) bool {
	return p&q == q
}

// ParsePerm разбирает строку вида "r-x".
func ParsePerm(s string) (Perm, error) {
	if len(s) != len(permLetters) {
		return 0, fmt.Errorf("perm %q: want 3 characters", s)
	}
	var p Perm
	for i, pl := range permLetters {
		switch s[i] {
		case pl.letter:
			p |= pl.flag
		case '-':
		default:
			return 0, fmt.Errorf("perm %q: unexpected %q at position %d", s, s[i], i+1)
		}
	}
	return p, nil
}
