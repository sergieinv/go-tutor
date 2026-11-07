---
title: Graceful shutdown
summary: srv.Shutdown — закрыть слушатели и дождаться активных запросов, ErrServerClosed, signal.NotifyContext, таймаут остановки, BaseContext, готовность для балансировщика и порядок остановки зависимостей.
difficulty: 4
minutes: 30
prerequisites:
  - 08-web/02-http-server
  - 08-web/07-request-context
  - 06-stdlib/22-signals-and-exit
docs:
  - title: "Пакет net/http: Server.Shutdown"
    url: https://pkg.go.dev/net/http#Server.Shutdown
  - title: "Пакет os/signal: NotifyContext"
    url: https://pkg.go.dev/os/signal#NotifyContext
  - title: "Kubernetes: Termination of Pods"
    url: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination
runner:
  race: true
---

Оркестратор останавливает сервис сигналом `SIGTERM` и ждёт (в Kubernetes по умолчанию 30 секунд), после чего добивает `SIGKILL`. За это время сервер должен перестать принимать новые соединения, дать доработать начатым запросам и закрыть зависимости. В Go это `srv.Shutdown(ctx)`: он закрывает слушатели, закрывает простаивающие keep-alive-соединения и ждёт, пока активные запросы закончатся. Если `ctx` истёк раньше, `Shutdown` возвращает его ошибку — тогда остаётся `srv.Close()`, который рвёт соединения.

Главная тонкость: `ListenAndServe`/`Serve` возвращают `http.ErrServerClosed` **сразу**, как только `Shutdown` закрыл слушатели, — запросы в этот момент ещё выполняются. Если `main` выйдет по возврату `Serve`, процесс завершится посреди ответов. Поэтому `main` ждёт именно завершения `Shutdown`, а `ErrServerClosed` считает нормальным исходом, а не ошибкой.

Остальное собирается из знакомых деталей: `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` превращает сигнал в отмену контекста; лимит на остановку — `context.WithTimeout` от `context.Background()` (контекст сигнала уже отменён); флаг готовности (`/ready` отвечает `503`) даёт балансировщику время убрать экземпляр из ротации до закрытия слушателя. Долгие запросы (long polling, стриминг) `Shutdown` не прерывает — сервер создают с `BaseContext`, который возвращает контекст, отменяемый вами, когда терпение кончилось. Соединения, захваченные через `Hijack` (WebSocket), `Shutdown` не ждёт и не закрывает: для них есть `srv.RegisterOnShutdown(f)`. Зависимости (база, очереди, трассировка) закрывают **после** сервера — хендлеры пользуются ими до последнего запроса.

## Как это в C#

- Generic Host делает всё это сам: ловит `SIGTERM`, вызывает `IHostApplicationLifetime.ApplicationStopping`, ждёт Kestrel и `IHostedService.StopAsync` не дольше `HostOptions.ShutdownTimeout` (по умолчанию 30 с; до .NET 6 было 5 с). В Go каждый шаг — несколько строк в `main`, порядок видно глазами.
- `ApplicationStopping` → отмена контекста из `signal.NotifyContext`; `ApplicationStopped` → момент, когда `Shutdown` вернул `nil`.
- Остановка зависимостей в .NET — `Dispose` контейнера DI в обратном порядке создания сервисов. В Go — явные вызовы (`db.Close()`) после `Shutdown`, обычно через `defer` в `main`, объявленные в порядке создания.
- Health checks (`MapHealthChecks` с тегом `ready`) — это обычный хендлер `/ready` с флагом `atomic.Bool`. Паузу перед закрытием слушателя .NET тоже не делает сам: Kestrel останавливается сразу после колбэков `ApplicationStopping`, так что задержку добавляют там (или в `IHostedLifecycleService.StoppingAsync`).

## Ловушки

### `main` выходит по возврату `Serve`

```go
go func() {
	<-ctx.Done()
	srv.Shutdown(context.Background())
}()
if err := srv.ListenAndServe(); err != http.ErrServerClosed {
	log.Fatal(err)
}
// main вернулся — процесс завершён, хотя Shutdown ещё ждёт запросы
```

`ListenAndServe` возвращается, как только закрыт слушатель. Запускайте `Serve` в горутине, а в `main` ждите сигнала и вызывайте `Shutdown` сами — тогда выход из `main` случится после него.

### Таймаут остановки от отменённого контекста

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
<-ctx.Done()
shCtx, cancel := context.WithTimeout(ctx, 10*time.Second) // ctx уже отменён!
srv.Shutdown(shCtx) // вернётся сразу с context.Canceled
```

Потомок отменённого контекста отменён с рождения, и `Shutdown` не ждёт ни секунды. Лимит на остановку берите от `context.Background()`.

### `BaseContext` от контекста сигнала

```go
srv := &http.Server{
	BaseContext: func(net.Listener) context.Context { return ctx }, // ctx из NotifyContext
}
```

Тогда сигнал сразу отменит контексты всех активных запросов: хендлеры прервут запросы к базе, и мягкая остановка превратится в жёсткую. Базовый контекст должен отменяться отдельно — когда истекло время на мягкую остановку.

## Задание

Напишите функцию запуска и остановки сервера:

```go
func Run(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration) error
```

Требования:

- до запуска `Run` записывает в `srv.BaseContext` функцию, возвращающую контекст `base`. `base` создаётся от `context.Background()` (не от `ctx`) и отменяется, когда мягкая остановка не уложилась в лимит (см. ниже); отмена `ctx` его не затрагивает;
- `srv.Serve(ln)` выполняется в отдельной горутине;
- если `Serve` вернулся раньше отмены `ctx`: при `http.ErrServerClosed` (сервер остановил кто-то другой) `Run` возвращает `nil`, при другой ошибке — её (так, чтобы `errors.Is` её находил);
- после отмены `ctx` вызывается `srv.Shutdown` с лимитом `timeout`, отсчитанным от момента отмены; при `timeout <= 0` лимита нет — `Shutdown` ждёт все запросы сколько потребуется;
- если `ctx` уже отменён при вызове `Run`, сервер всё равно запускается и сразу проходит ту же мягкую остановку;
- если `Shutdown` вернул `nil`, `Run` возвращает `nil`; другую ошибку `Shutdown` (не истечение лимита) `Run` возвращает так, чтобы `errors.Is` её находил;
- если лимит истёк, `Run` отменяет `base`, вызывает `srv.Close()` и возвращает ошибку, для которой `errors.Is(err, context.DeadlineExceeded)` истинно;
- `Run` возвращается только после того, как `Serve` вернулся (горутина не остаётся висеть, слушатель закрыт).
