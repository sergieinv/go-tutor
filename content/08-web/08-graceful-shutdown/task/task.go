package task

import (
	"context"
	"net"
	"net/http"
	"time"
)

// Run обслуживает ln сервером srv, пока не отменён ctx,
// затем мягко останавливает сервер не дольше timeout.
func Run(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration) error {
	// TODO: реализуйте функцию
	return nil
}
