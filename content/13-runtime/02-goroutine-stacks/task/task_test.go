package task

import (
	"bytes"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
)

// Лимит стека в «глубоких» тестах: 256 КБ — как будто вход в тысячи
// раз больше, а стек ограничен по умолчанию (1 ГБ).
const stackLimit = 256 << 10

const childEnv = "TASK_STACK_CHILD"

// deep запускает тест в дочернем процессе с маленьким лимитом стека.
// Переполнение стека — fatal error: оно убивает процесс целиком,
// и recover его не ловит. Поэтому тест перезапускает сам себя:
// падает только дочерний процесс, а здесь мы объясняем, что случилось.
// В дочернем процессе deep возвращает true — выполняйте проверку.
func deep(t *testing.T) bool {
	t.Helper()
	if os.Getenv(childEnv) == t.Name() {
		debug.SetMaxStack(stackLimit)
		return true
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), childEnv+"="+t.Name())
	out, err := cmd.CombinedOutput()
	if err == nil && bytes.Contains(out, []byte("--- PASS: "+t.Name())) {
		return false
	}
	if bytes.Contains(out, []byte("goroutine stack exceeds")) {
		t.Fatalf("fatal error: stack overflow — рекурсия съела стек (лимит в тесте %d КБ).\n"+
			"Глубина рекурсии растёт вместе со входом; храните путь обхода в срезе-стеке в куче.", stackLimit>>10)
	}
	t.Fatalf("дочерний процесс с лимитом стека завершился с ошибкой: %v\n%s", err, tail(out))
	return false
}

// tail — последние строки вывода, без простыни стеков горутин.
func tail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	return strings.Join(lines, "\n")
}

func chain(n int) *Node {
	var root *Node
	for range n {
		root = &Node{Left: root}
	}
	return root
}

func grid(rows ...string) [][]byte {
	g := make([][]byte, len(rows))
	for i, r := range rows {
		g[i] = []byte(r)
	}
	return g
}

func rows(g [][]byte) []string {
	out := make([]string, len(g))
	for i, r := range g {
		out[i] = string(r)
	}
	return out
}

func TestMaxDepthSmall(t *testing.T) {
	tree := &Node{Left: &Node{Right: &Node{}}, Right: &Node{}}
	if got := MaxDepth(tree); got != 3 {
		t.Errorf("MaxDepth = %d, want 3", got)
	}
	if got := MaxDepth(nil); got != 0 {
		t.Errorf("MaxDepth(nil) = %d, want 0", got)
	}
}

func TestFloodSmall(t *testing.T) {
	g := grid(
		"..#..",
		".##..",
		"#....",
	)
	n := Flood(g, 0, 0, 'o')
	want := []string{
		"oo#..",
		"o##..",
		"#....",
	}
	if n != 3 || strings.Join(rows(g), "\n") != strings.Join(want, "\n") {
		t.Fatalf("Flood(0,0) = %d, сетка:\n%s\nwant 3, сетка:\n%s", n, strings.Join(rows(g), "\n"), strings.Join(want, "\n"))
	}
}

func TestMaxDepthDeep(t *testing.T) {
	if !deep(t) {
		return
	}
	if got := MaxDepth(chain(1_000_000)); got != 1_000_000 {
		t.Fatalf("MaxDepth(цепочка из 1000000) = %d", got)
	}
}

func TestFloodDeep(t *testing.T) {
	if !deep(t) {
		return
	}
	g := make([][]byte, 1000)
	for i := range g {
		g[i] = bytes.Repeat([]byte{'.'}, 1000)
	}
	if n := Flood(g, 500, 500, 'o'); n != 1_000_000 {
		t.Fatalf("Flood сплошного поля 1000×1000 = %d, want 1000000", n)
	}
}
