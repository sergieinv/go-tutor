package task

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Run обслуживает ln сервером srv, пока не отменён ctx,
// затем мягко останавливает сервер не дольше timeout.
func Run(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration) error {
	// Контексты запросов — от своего base, а не от ctx: иначе
	// сигнал остановки оборвал бы все активные запросы сразу.
	base, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv.BaseContext = func(net.Listener) context.Context { return base }

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	// ctx уже отменён — лимит на остановку отсчитываем от Background.
	shCtx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		shCtx, cancel = context.WithTimeout(shCtx, timeout)
		defer cancel()
	}
	err := srv.Shutdown(shCtx)
	if err != nil {
		// Не уложились: отменяем контексты запросов и рвём соединения.
		cancelBase()
		srv.Close()
		err = fmt.Errorf("shutdown: %w", err)
	}
	<-serveErr // Serve вернул ErrServerClosed, горутина завершилась
	return err
}
