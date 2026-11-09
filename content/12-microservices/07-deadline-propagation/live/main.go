// Живой визуализатор: три запроса проходят цепочку gateway → orders →
// warehouse. Каждый сервис — задача runtime/trace (дочерний вызов —
// строкой ниже), его работа — регион, остаток бюджета — запись trace.Log.
//
// Запрос 1 укладывается в бюджет, а после ответа orders запускает фоновый
// аудит с context.WithoutCancel. Запросу 2 времени мало: orders отказывает,
// не вызывая склад. Запрос 3 клиент отменяет, и отмена доходит до склада.
//
// Попробуйте в callNext заменить context.WithTimeout(ctx, child) на
// context.WithTimeout(context.Background(), child) и запустить снова:
// склад запроса 3 не узнает об отмене и доработает до конца, а его задача
// станет отдельной строкой без родителя — цепочка контекстов разорвана.
package main

import (
	"context"
	"errors"
	"fmt"
	"runtime/trace"
	"sync"
	"time"
)

var errNoTime = errors.New("не хватит времени")

// left — остаток до дедлайна с точностью до 10 мс.
func left(ctx context.Context) (time.Duration, string) {
	dl, ok := ctx.Deadline()
	if !ok {
		return time.Hour, "дедлайна нет"
	}
	d := time.Until(dl)
	return d, fmt.Sprintf("осталось %d мс", d.Round(10*time.Millisecond).Milliseconds())
}

type service struct {
	name    string
	need    time.Duration // меньше этого остатка — отказ сразу
	work    time.Duration
	reserve time.Duration // запас себе после ответа соседа
	next    *service
	root    bool // задачу trace уже создал вызывающий (вход запроса)
}

func (s *service) handle(ctx context.Context) error {
	if !s.root {
		var task *trace.Task
		ctx, task = trace.NewTask(ctx, s.name)
		defer task.End()
	}
	d, txt := left(ctx)
	trace.Log(ctx, "deadline", txt)
	if d < s.need {
		trace.Log(ctx, "отказ", fmt.Sprintf("нужно %d мс — не начинаем", s.need.Milliseconds()))
		return errNoTime
	}
	trace.WithRegion(ctx, "работа", func() {
		select {
		case <-time.After(s.work):
		case <-ctx.Done():
		}
	})
	if err := ctx.Err(); err != nil {
		trace.Log(ctx, "отмена", err.Error())
		return err
	}
	if s.next == nil {
		return nil
	}
	return s.callNext(ctx)
}

// callNext даёт соседу текущий остаток минус свой запас и не зовёт его,
// если тому не хватит времени.
func (s *service) callNext(ctx context.Context) error {
	d, _ := left(ctx) // остаток уже после своей работы
	child := d - s.reserve
	if child < s.next.need {
		trace.Log(ctx, "отказ", fmt.Sprintf("соседу осталось бы %d мс — не зовём", child.Milliseconds()))
		return errNoTime
	}
	ctx, cancel := context.WithTimeout(ctx, child)
	defer cancel()
	return s.next.handle(ctx)
}

func main() {
	warehouse := &service{name: "warehouse", need: 60 * time.Millisecond, work: 40 * time.Millisecond}
	orders := &service{name: "orders", need: 80 * time.Millisecond, work: 20 * time.Millisecond, reserve: 40 * time.Millisecond, next: warehouse}
	gateway := &service{name: "gateway", work: 10 * time.Millisecond, reserve: 10 * time.Millisecond, next: orders, root: true}

	var wg sync.WaitGroup
	request := func(n int, budget time.Duration, cancelAfter time.Duration) {
		ctx, task := trace.NewTask(context.Background(), fmt.Sprintf("gateway · запрос %d", n))
		defer task.End()
		ctx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()
		if cancelAfter > 0 {
			t := time.AfterFunc(cancelAfter, cancel) // клиент закрыл вкладку
			defer t.Stop()
		}
		err := gateway.handle(ctx)
		trace.Log(ctx, "ответ", fmt.Sprint(err))
		fmt.Printf("запрос %d: %v\n", n, err)
		if err == nil {
			// Фоновая работа после ответа: значения контекста сохраняются,
			// отмена и дедлайн запроса — нет. Свой таймаут обязателен.
			bg, stop := context.WithTimeout(context.WithoutCancel(ctx), 50*time.Millisecond)
			wg.Go(func() {
				defer stop()
				bg, task := trace.NewTask(bg, "аудит (фон)")
				defer task.End()
				trace.WithRegion(bg, "запись", func() { time.Sleep(30 * time.Millisecond) })
				trace.Log(bg, "фон", "запрос уже отменён, а фон — нет: "+fmt.Sprint(bg.Err()))
			})
		}
	}

	request(1, 200*time.Millisecond, 0)
	request(2, 120*time.Millisecond, 0)
	request(3, 300*time.Millisecond, 50*time.Millisecond)
	wg.Wait()
}
