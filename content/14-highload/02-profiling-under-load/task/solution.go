package task

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Capture сохраняет снимок трассы после медленного запроса: не чаще
// раза в Cooldown и не больше одного снимка одновременно.
type Capture struct {
	Threshold time.Duration
	Cooldown  time.Duration
	Now       func() time.Time
	Snapshot  func(reason string) error

	mu       sync.Mutex
	busy     bool      // выполняется Snapshot
	tried    bool      // попытки уже были
	last     time.Time // момент последней попытки
	captured int64
}

func (c *Capture) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Middleware меряет время запроса и при необходимости делает снимок.
func (c *Capture) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := c.now()
		next.ServeHTTP(w, r)
		end := c.now()
		elapsed := end.Sub(start)
		if c.Threshold <= 0 || elapsed < c.Threshold || c.Snapshot == nil {
			return
		}
		if !c.begin(end) {
			return
		}
		err := c.Snapshot(fmt.Sprintf("%s %s %v", r.Method, r.URL.Path, elapsed))
		c.mu.Lock()
		c.busy = false
		if err == nil {
			c.captured++
		}
		c.mu.Unlock()
	})
}

// begin занимает право на снимок, если оно свободно и cooldown прошёл.
func (c *Capture) begin(at time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy {
		return false
	}
	if c.tried && c.Cooldown > 0 && at.Sub(c.last) < c.Cooldown {
		return false
	}
	c.busy, c.tried, c.last = true, true, at
	return true
}

// Captured возвращает число удачных снимков.
func (c *Capture) Captured() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.captured
}
