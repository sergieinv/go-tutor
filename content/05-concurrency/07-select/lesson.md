---
title: Select
summary: Ожидание нескольких каналов сразу, таймауты, неблокирующие операции и выключение веток nil-каналом.
difficulty: 3
minutes: 20
prerequisites:
  - 05-concurrency/02-channels
  - 05-concurrency/06-channel-directions
docs:
  - title: "Спецификация: Select statements"
    url: https://go.dev/ref/spec#Select_statements
  - title: "Effective Go: Channels"
    url: https://go.dev/doc/effective_go#channels
  - title: "Пакет time: After"
    url: https://pkg.go.dev/time#After
runner:
  race: true
---

`select` похож на `switch`, но каждая ветка — операция с каналом: получение `<-ch` или отправка `ch <- v`. Горутина блокируется, пока хотя бы одна ветка не станет готова, и выполняет ровно одну из них.

На `select` строятся таймауты, отмена, неблокирующие проверки и мультиплексирование нескольких источников — всё, для чего в C# используют `Task.WhenAny`, `CancellationToken` и `Channel<T>.Reader.TryRead`.

## Как это в C#

- `Task.WhenAny(a, b)` ждёт завершения одной из задач. `select` ждёт готовности одного из каналов и сразу забирает значение — без отдельного `await` на победителе.
- Таймаут в C# — гонка с `Task.Delay`. В Go — ветка `case <-time.After(d)`.
- `TryRead` / `TryWrite` у `System.Threading.Channels` — это `select` с веткой `default`.
- Если к вызову `Task.WhenAny` уже завершены несколько задач, вернётся первая по списку. В Go выбор среди готовых веток случаен по спецификации — порядок `case` не даёт приоритета.

## Ловушки

### Закрытый канал всегда готов

Получение из закрытого канала мгновенно возвращает нулевое значение. Если оставить такую ветку в цикле, `select` будет выбирать её снова и снова:

```go
for {
	select {
	case v, ok := <-a:
		if !ok {
			a = nil // nil-канал никогда не готов — ветка выключена
			continue
		}
		fmt.Println(v)
	case v := <-b:
		fmt.Println(v)
	}
}
```

### `time.After` в цикле сбрасывает таймаут

```go
for {
	select {
	case v := <-events:
		handle(v)
	case <-time.After(time.Second): // новый таймер на каждой итерации
		return
	}
}
```

Это таймаут «тишины», а не общий лимит времени: пока события приходят чаще раза в секунду, цикл не закончится никогда. Для общего лимита создайте таймер один раз до цикла или используйте `context.WithTimeout`.

### Пустой `default` в цикле — это busy loop

`select` с веткой `default` внутри `for` без паузы крутится вхолостую и занимает ядро процессора на 100%. Если ждать нечего, уберите `default` — тогда горутина честно заснёт до готовности канала.

## Задание

Напишите функцию `Race(a, b <-chan string, timeout time.Duration) (string, error)`, которая возвращает первое значение, пришедшее из `a` или `b`.

Требования:

- если канал закрыт, он больше не участвует в гонке — ждём второй;
- если оба канала закрыты, сразу возвращаем `ErrNoResult`;
- если за `timeout` ничего не пришло, возвращаем `ErrTimeout`; `timeout` — общий лимит на весь вызов;
- `nil`-канал считается источником, который никогда ничего не пришлёт.
