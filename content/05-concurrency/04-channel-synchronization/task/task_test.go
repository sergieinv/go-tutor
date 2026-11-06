package task

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// goid возвращает номер текущей горутины из заголовка стека.
func goid() string {
	buf := make([]byte, 64)
	buf = buf[:runtime.Stack(buf, false)]
	return strings.Fields(string(buf))[1]
}

// journal пишет вызовы без мьютекса: если очерёдность не передаётся
// через каналы, -race сообщит о гонке.
type journal struct {
	log  []string
	aIDs map[string]bool
	bIDs map[string]bool
}

func newJournal() *journal {
	return &journal{aIDs: map[string]bool{}, bIDs: map[string]bool{}}
}

func (j *journal) a(i int) {
	j.log = append(j.log, fmt.Sprint("a", i))
	j.aIDs[goid()] = true
}

func (j *journal) b(i int) {
	j.log = append(j.log, fmt.Sprint("b", i))
	j.bIDs[goid()] = true
}

func TestAlternateOrder(t *testing.T) {
	tests := []struct {
		n    int
		want []string
	}{
		{1, []string{"a0", "b0"}},
		{3, []string{"a0", "b0", "a1", "b1", "a2", "b2"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint("n=", tt.n), func(t *testing.T) {
			j := newJournal()
			Alternate(tt.n, j.a, j.b)
			if !slices.Equal(j.log, tt.want) {
				t.Errorf("порядок вызовов = %v, want %v", j.log, tt.want)
			}
		})
	}
}
