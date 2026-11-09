// Четыре горутины одновременно кладут по значению в стек
// Трайбера. Каждый Push — задача trace.NewTask, каждая попытка
// CAS — регион, а неудача и успех — записи trace.Log. Между
// чтением вершины и CAS стоит пауза, чтобы попытки
// пересекались: видно, как проигравшие перечитывают вершину и
// повторяют. Уберите паузу или поменяйте GOMAXPROCS — и
// запустите снова.
package main

import (
	"context"
	"fmt"
	"runtime/trace"
	"sync"
	"sync/atomic"
	"time"
)

type node struct {
	v    int
	next *node
}

type Stack struct {
	head atomic.Pointer[node]
}

// gap — работа между Load и CAS: в настоящем коде её почти нет,
// здесь она растягивает окно, в которое вмешиваются другие.
const gap = 2 * time.Millisecond

func (s *Stack) Push(ctx context.Context, v int) {
	ctx, task := trace.NewTask(ctx, fmt.Sprintf("Push(%d)", v))
	defer task.End()

	n := &node{v: v}
	for attempt := 1; ; attempt++ {
		ok := false
		trace.WithRegion(ctx, fmt.Sprintf("попытка %d", attempt), func() {
			old := s.head.Load()
			n.next = old
			time.Sleep(gap)
			ok = s.head.CompareAndSwap(old, n)
		})
		if ok {
			trace.Log(ctx, "cas", fmt.Sprintf("успех с попытки %d", attempt))
			return
		}
		trace.Log(ctx, "cas", "не прошёл: вершину сменила другая горутина, перечитываем")
	}
}

func (s *Stack) Pop() (int, bool) {
	for {
		old := s.head.Load()
		if old == nil {
			return 0, false
		}
		if s.head.CompareAndSwap(old, old.next) {
			return old.v, true
		}
	}
}

func main() {
	ctx := context.Background()
	var s Stack
	var wg sync.WaitGroup
	for v := 1; v <= 4; v++ {
		wg.Go(func() { s.Push(ctx, v) })
	}
	wg.Wait()

	// Порядок в стеке зависит от того, кто выиграл CAS первым,
	// но каждое значение лежит в нём ровно один раз.
	seen := map[int]bool{}
	for {
		v, ok := s.Pop()
		if !ok {
			break
		}
		seen[v] = true
	}
	fmt.Println("в стеке было значений:", len(seen))
}
