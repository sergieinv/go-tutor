package task

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// newHealth поднимает gRPC-сервер с health-сервисом и возвращает
// сам health-сервер и клиент к нему по bufconn.
func newHealth(t *testing.T) (*health.Server, healthpb.HealthClient) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	hs := health.NewServer()
	healthpb.RegisterHealthServer(srv, hs)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return hs, healthpb.NewHealthClient(conn)
}

// statusOf спрашивает статус сервиса через настоящий вызов Check.
func statusOf(t *testing.T, hc healthpb.HealthClient, service string) string {
	t.Helper()
	resp, err := hc.Check(context.Background(), &healthpb.HealthCheckRequest{Service: service})
	if err != nil {
		return status.Code(err).String()
	}
	return resp.GetStatus().String()
}

func ok(context.Context) error   { return nil }
func down(context.Context) error { return errors.New("connection refused") }

func TestAllHealthy(t *testing.T) {
	hs, hc := newHealth(t)
	m := NewMonitor(hs,
		[]Dependency{{"postgres", ok}, {"kafka", ok}},
		map[string][]string{"shop.v1.OrderService": {"postgres", "kafka"}, "shop.v1.Admin": {}},
		time.Second)
	if failed := m.Refresh(context.Background()); len(failed) != 0 {
		t.Errorf("Refresh = %q, want пусто", failed)
	}
	for _, svc := range []string{"", "shop.v1.OrderService", "shop.v1.Admin"} {
		if got := statusOf(t, hc, svc); got != "SERVING" {
			t.Errorf("Check(%q) = %s, want SERVING", svc, got)
		}
	}
}

func TestDatabaseDown(t *testing.T) {
	hs, hc := newHealth(t)
	m := NewMonitor(hs,
		[]Dependency{{"postgres", down}, {"kafka", ok}},
		map[string][]string{"shop.v1.OrderService": {"postgres"}, "shop.v1.Notify": {"kafka"}},
		time.Second)
	if failed := m.Refresh(context.Background()); !slices.Equal(failed, []string{"postgres"}) {
		t.Errorf("Refresh = %q, want [postgres]", failed)
	}
	want := map[string]string{"": "NOT_SERVING", "shop.v1.OrderService": "NOT_SERVING", "shop.v1.Notify": "SERVING"}
	for svc, w := range want {
		if got := statusOf(t, hc, svc); got != w {
			t.Errorf("Check(%q) = %s, want %s", svc, got, w)
		}
	}
}
