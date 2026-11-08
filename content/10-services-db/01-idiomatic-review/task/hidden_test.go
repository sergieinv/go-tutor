package task

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// call вызывает f и превращает панику в провал теста, а не
// в падение всего пакета.
func call(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s паникует: %v — библиотека должна вернуть ошибку", what, r)
		}
	}()
	f()
}

func checkMessage(t *testing.T, err error, mustContain string) {
	t.Helper()
	msg := err.Error()
	if !strings.Contains(msg, mustContain) {
		t.Errorf("текст ошибки %q не называет %q — по нему не понять, что сломалось", msg, mustContain)
	}
	if r, _ := utf8.DecodeRuneInString(msg); unicode.IsUpper(r) {
		t.Errorf("текст ошибки %q начинается с заглавной: его допишут в середину чужой фразы", msg)
	}
	if strings.HasSuffix(msg, ".") || strings.HasSuffix(msg, "!") {
		t.Errorf("текст ошибки %q кончается знаком препинания", msg)
	}
}

func TestReviewNoPanic(t *testing.T) {
	src := newSource()
	c := newCatalog(t, src, time.Hour)
	var err error
	call(t, `Price("")`, func() { _, err = c.Price(t.Context(), "") })
	if !errors.Is(err, ErrInvalidSKU) {
		t.Errorf(`Price("") error = %v, want ErrInvalidSKU`, err)
	}
	var sum int64
	call(t, `Total([apple ""])`, func() { sum, err = c.Total(t.Context(), []string{"apple", ""}) })
	if !errors.Is(err, ErrInvalidSKU) || sum != 0 {
		t.Errorf(`Total([apple ""]) = (%d, %v), want (0, ErrInvalidSKU)`, sum, err)
	}
}

func TestReviewWrap(t *testing.T) {
	c := newCatalog(t, newSource(), time.Hour)
	p, err := c.Price(t.Context(), "ghost")
	if p != 0 || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Price(ghost) = (%d, %v), want (0, ошибка с errors.Is(err, ErrNotFound))", p, err)
	}
	checkMessage(t, err, "ghost")
}

func TestReviewNoLog(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	c := newCatalog(t, newSource(), time.Hour)
	if _, err := c.Price(t.Context(), "ghost"); err == nil {
		t.Fatal("Price(ghost) без ошибки")
	}
	if _, err := c.Total(t.Context(), []string{"ghost"}); err == nil {
		t.Fatal("Total([ghost]) без ошибки")
	}
	if buf.Len() > 0 {
		t.Errorf("каталог и вернул ошибку, и записал её в лог: %q — логирует тот, кто обрабатывает", buf.String())
	}
}

type ctxKey struct{}

func TestReviewContext(t *testing.T) {
	src := newSource()
	c := newCatalog(t, src, time.Hour)
	ctx := context.WithValue(t.Context(), ctxKey{}, "req-42")
	if _, err := c.Price(ctx, "apple"); err != nil {
		t.Fatalf("Price(apple) = %v", err)
	}
	src.mu.Lock()
	got := src.lastCtx
	src.mu.Unlock()
	if got == nil || got.Value(ctxKey{}) != "req-42" {
		t.Error("источник получил не тот контекст, с которым вызвали Price")
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	p, err := c.Price(canceled, "pear")
	if p != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("Price(pear) с отменённым контекстом = (%d, %v), want (0, context.Canceled)", p, err)
	}
	if _, err := c.Total(canceled, []string{"plum"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Total([plum]) с отменённым контекстом: error = %v, want context.Canceled", err)
	}
}

func TestReviewTotalError(t *testing.T) {
	c := newCatalog(t, newSource(), time.Hour)
	sum, err := c.Total(t.Context(), []string{"apple", "ghost", "pear"})
	if sum != 0 || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Total([apple ghost pear]) = (%d, %v), want (0, ErrNotFound) — ошибку нельзя проглатывать", sum, err)
	}
	checkMessage(t, err, "ghost")
}

func TestReviewClose(t *testing.T) {
	before := runtime.NumGoroutine()
	c := NewCatalog(newSource(), time.Millisecond)
	if _, err := c.Price(t.Context(), "apple"); err != nil {
		t.Fatalf("Price(apple) = %v", err)
	}
	c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("после Close горутин %d, до NewCatalog было %d: фоновая горутина не остановилась", runtime.NumGoroutine(), before)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestErrorNotCached(t *testing.T) {
	src := newSource()
	c := newCatalog(t, src, time.Hour)
	c.Price(t.Context(), "kiwi")
	src.mu.Lock()
	src.prices["kiwi"] = 300
	src.mu.Unlock()
	if p, err := c.Price(t.Context(), "kiwi"); p != 300 || err != nil {
		t.Errorf("Price(kiwi) после появления цены = (%d, %v), want (300, nil): ошибки не кешируются", p, err)
	}
}

func TestConcurrent(t *testing.T) {
	c := newCatalog(t, newSource(), time.Millisecond)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for range 50 {
				if i%2 == 0 {
					if p, err := c.Price(t.Context(), "plum"); p != 45 || err != nil {
						t.Errorf("Price(plum) = (%d, %v)", p, err)
						return
					}
				} else if s, err := c.Total(t.Context(), []string{"apple", "pear"}); s != 210 || err != nil {
					t.Errorf("Total = (%d, %v)", s, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
