package task

import (
	"context"
	"errors"
	"testing"
	"time"
)

// after отвечает value через d или возвращает ошибку контекста при отмене.
func after(d time.Duration, value string) Backend {
	return func(ctx context.Context) (string, error) {
		select {
		case <-time.After(d):
			return value, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func failing(err error) Backend {
	return func(context.Context) (string, error) { return "", err }
}

func TestSearchFirstWins(t *testing.T) {
	got, err := Search(context.Background(), time.Second, after(time.Second, "slow"), after(10*time.Millisecond, "fast"))
	if err != nil || got != "fast" {
		t.Fatalf("Search = (%q, %v), want (\"fast\", nil)", got, err)
	}
}

func TestSearchAllFail(t *testing.T) {
	e1, e2 := errors.New("db down"), errors.New("cache miss")
	_, err := Search(context.Background(), time.Second, failing(e1), failing(e2))
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("Search вернул %v, want ошибку с обеими причинами", err)
	}
}

func TestSearchTimeout(t *testing.T) {
	_, err := Search(context.Background(), 30*time.Millisecond, after(time.Hour, "never"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Search вернул %v, want context.DeadlineExceeded", err)
	}
}
