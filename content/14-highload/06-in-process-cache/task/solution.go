package task

import (
	"container/list"
	"sync"
	"time"
)

type item[K comparable, V any] struct {
	key K
	val V
	exp time.Time // нулевое — без срока
}

type Cache[K comparable, V any] struct {
	mu  sync.Mutex
	cap int
	ttl time.Duration
	now func() time.Time
	ll  *list.List // от свежих к старым
	m   map[K]*list.Element
}

func New[K comparable, V any](capacity int, ttl time.Duration, now func() time.Time) *Cache[K, V] {
	if now == nil {
		now = time.Now
	}
	return &Cache[K, V]{
		cap: max(capacity, 1),
		ttl: ttl,
		now: now,
		ll:  list.New(),
		m:   make(map[K]*list.Element),
	}
}

func (c *Cache[K, V]) expired(it *item[K, V], now time.Time) bool {
	return c.ttl > 0 && !now.Before(it.exp)
}

func (c *Cache[K, V]) remove(el *list.Element) {
	c.ll.Remove(el)
	delete(c.m, el.Value.(*item[K, V]).key)
}

func (c *Cache[K, V]) Get(k K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	el, ok := c.m[k]
	if !ok {
		return zero, false
	}
	it := el.Value.(*item[K, V])
	if c.expired(it, c.now()) {
		c.remove(el)
		return zero, false
	}
	c.ll.MoveToFront(el)
	return it.val, true
}

func (c *Cache[K, V]) Set(k K, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var exp time.Time
	if c.ttl > 0 {
		exp = c.now().Add(c.ttl)
	}
	if el, ok := c.m[k]; ok {
		it := el.Value.(*item[K, V])
		it.val, it.exp = v, exp
		c.ll.MoveToFront(el)
		return
	}
	c.m[k] = c.ll.PushFront(&item[K, V]{key: k, val: v, exp: exp})
	if c.ll.Len() > c.cap {
		c.remove(c.ll.Back())
	}
}

func (c *Cache[K, V]) Delete(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[k]; ok {
		c.remove(el)
	}
}

func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now, n := c.now(), 0
	for el := c.ll.Front(); el != nil; el = el.Next() {
		if !c.expired(el.Value.(*item[K, V]), now) {
			n++
		}
	}
	return n
}

func (c *Cache[K, V]) Purge() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now, n := c.now(), 0
	for el := c.ll.Front(); el != nil; {
		next := el.Next()
		if c.expired(el.Value.(*item[K, V]), now) {
			c.remove(el)
			n++
		}
		el = next
	}
	return n
}
