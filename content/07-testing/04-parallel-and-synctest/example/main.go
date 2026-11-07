// Кеш с временем жизни записей и фоновой уборкой. Время
// берётся прямо из `time.Now` и `time.Ticker` — без
// интерфейса «часов»: в тестах время подменит `synctest`.

package cache

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	val     string
	expires time.Time
}

// Cache безопасен для одновременного использования.
type Cache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]entry
}

func New(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, items: map[string]entry{}}
}

func (c *Cache) Set(key, val string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = entry{val, time.Now().Add(c.ttl)}
}

// Get находит запись, пока не наступил момент `expires`.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || !time.Now().Before(e.expires) {
		return "", false
	}
	return e.val, true
}

func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Janitor раз в every удаляет протухшие записи,
// пока не отменён ctx.
func (c *Cache) Janitor(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			c.mu.Lock()
			for k, e := range c.items {
				if !now.Before(e.expires) {
					delete(c.items, k)
				}
			}
			c.mu.Unlock()
		}
	}
}
