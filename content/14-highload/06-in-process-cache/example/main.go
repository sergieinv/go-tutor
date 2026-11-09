// Три кеша в памяти процесса, от простого к быстрому: map под мьютексом
// со сроком жизни записей, LRU с ограничением размера и шардированный
// кеш, где одна блокировка не тормозит все ядра. Пример запускается
// тестами: `go test -v` проверяет поведение, кнопка «Бенчмарки»
// сравнивает один мьютекс и шарды под параллельной нагрузкой.
package main

import (
	"container/list"
	"sync"
	"time"
)

func main() {}

// TTL-кеш: запись живёт `ttl` с момента записи. Время внедрено функцией
// `now`, чтобы тесты двигали его, а не спали.
type entry[V any] struct {
	val V
	exp time.Time
}

type TTLCache[K comparable, V any] struct {
	mu  sync.RWMutex
	m   map[K]entry[V]
	ttl time.Duration
	now func() time.Time
}

func NewTTL[K comparable, V any](ttl time.Duration, now func() time.Time) *TTLCache[K, V] {
	return &TTLCache[K, V]{m: make(map[K]entry[V]), ttl: ttl, now: now}
}

func (c *TTLCache[K, V]) Set(k K, v V) {
	c.mu.Lock()
	c.m[k] = entry[V]{val: v, exp: c.now().Add(c.ttl)}
	c.mu.Unlock()
}

// Ленивое удаление: истёкшая запись — промах. Удалить её под `RLock`
// нельзя, поэтому берём полную блокировку и проверяем ещё раз:
// между ними другая горутина могла записать свежее значение.
func (c *TTLCache[K, V]) Get(k K) (V, bool) {
	c.mu.RLock()
	e, ok := c.m[k]
	c.mu.RUnlock()
	if ok && c.now().Before(e.exp) {
		return e.val, true
	}
	if ok {
		c.mu.Lock()
		if e, ok := c.m[k]; ok && !c.now().Before(e.exp) {
			delete(c.m, k)
		}
		c.mu.Unlock()
	}
	var zero V
	return zero, false
}

// Ключи, которые больше не читают, ленивое удаление не тронет никогда —
// поэтому нужна и фоновая чистка. Обход всей map под блокировкой:
// на миллионах записей делайте её порциями или по шардам.
func (c *TTLCache[K, V]) Sweep() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for k, e := range c.m {
		if !c.now().Before(e.exp) {
			delete(c.m, k)
			n++
		}
	}
	return n
}

// Фоновая горутина чистки с явной остановкой: кеш, который запускает
// горутину, обязан уметь её остановить. Тики приходят из канала —
// в проде это `time.NewTicker(time.Minute).C`, в тесте — свой канал.
func (c *TTLCache[K, V]) StartJanitor(tick <-chan time.Time) (stop func()) {
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-tick:
				c.Sweep()
			case <-quit:
				return
			}
		}
	}()
	return func() { close(quit); <-done }
}

func (c *TTLCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m)
}

// LRU: двусвязный список от свежих к старым + map ключ → элемент списка.
// Чтение переносит элемент в начало, переполнение вытесняет хвост.
// Без мьютекса: даже `Get` меняет список, так что под `RWMutex`
// LRU не ускорить — читатели тоже пишут.
type lruItem[K comparable, V any] struct {
	key K
	val V
}

type LRU[K comparable, V any] struct {
	cap int
	ll  *list.List
	m   map[K]*list.Element
}

func NewLRU[K comparable, V any](capacity int) *LRU[K, V] {
	return &LRU[K, V]{cap: capacity, ll: list.New(), m: make(map[K]*list.Element, capacity)}
}

func (c *LRU[K, V]) Get(k K) (V, bool) {
	if el, ok := c.m[k]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*lruItem[K, V]).val, true
	}
	var zero V
	return zero, false
}

// Set возвращает вытесненный ключ, чтобы его было видно в тестах.
func (c *LRU[K, V]) Set(k K, v V) (evicted K, ok bool) {
	if el, found := c.m[k]; found {
		el.Value.(*lruItem[K, V]).val = v
		c.ll.MoveToFront(el)
		return evicted, false
	}
	c.m[k] = c.ll.PushFront(&lruItem[K, V]{key: k, val: v})
	if c.ll.Len() <= c.cap {
		return evicted, false
	}
	old := c.ll.Remove(c.ll.Back()).(*lruItem[K, V])
	delete(c.m, old.key)
	return old.key, true
}

// Шардированный кеш: 16 независимых map, каждая под своим мьютексом.
// Шард выбирается хешем ключа (FNV-1a — без аллокаций и детерминирован),
// так что горутины с разными ключами почти не спорят за блокировку.
const shards = 16

type Sharded[V any] struct {
	s [shards]struct {
		sync.Mutex
		m map[string]V
	}
}

func NewSharded[V any]() *Sharded[V] {
	c := &Sharded[V]{}
	for i := range c.s {
		c.s[i].m = make(map[string]V)
	}
	return c
}

func shardOf(key string) int {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % shards)
}

func (c *Sharded[V]) Get(k string) (V, bool) {
	sh := &c.s[shardOf(k)]
	sh.Lock()
	v, ok := sh.m[k]
	sh.Unlock()
	return v, ok
}

func (c *Sharded[V]) Set(k string, v V) {
	sh := &c.s[shardOf(k)]
	sh.Lock()
	sh.m[k] = v
	sh.Unlock()
}

// Для сравнения в бенчмарке — одна map под одним мьютексом.
type Locked[V any] struct {
	mu sync.Mutex
	m  map[string]V
}

func (c *Locked[V]) Get(k string) (V, bool) {
	c.mu.Lock()
	v, ok := c.m[k]
	c.mu.Unlock()
	return v, ok
}
