## Подсказка 1

Создайте `base, cancelBase := context.WithCancel(context.Background())` и `defer cancelBase()`, затем `srv.BaseContext = func(net.Listener) context.Context { return base }` — до запуска `Serve`, он читает поле при старте. `Serve` запускайте в горутине, которая отправляет результат в канал с буфером 1.

## Подсказка 2

Ждите в `select` либо результат `Serve`, либо `ctx.Done()`. Если первым пришёл `Serve` — разберите ошибку через `errors.Is(err, http.ErrServerClosed)`. Если `ctx` — создайте контекст остановки от `context.Background()`: при `timeout > 0` — `WithTimeout`, иначе сам `Background`.

## Подсказка 3

```go
err := srv.Shutdown(shCtx)
if err != nil {
	cancelBase()
	srv.Close()
	err = fmt.Errorf("shutdown: %w", err)
}
<-serveErr // дождаться горутины с Serve
return err
```
