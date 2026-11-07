## Подсказка 1

Состояние под `sync.Mutex`: запланирован ли вызов (`pending`), когда началась серия (`first`), выключен ли дебаунсер и текущий таймер. Таймер с колбэком в своей горутине даёт `time.AfterFunc(d, func() {...})`; `timer.Stop()` отменяет его, если он ещё не сработал. В `synctest` и `time.Now`, и `AfterFunc` работают на фейковых часах — ничего подменять не нужно.

## Подсказка 2

В `Trigger`: если `pending` ложно — начинается серия, запомните `first = time.Now()`. Момент вызова — `now + wait`, а при `maxWait > 0` — не позже `first + maxWait`. Остановите старый таймер и заведите новый на `at.Sub(now)`. В колбэке сбросьте `pending` и вызовите `f` уже **после** `Unlock` — иначе `f`, которая сама вызывает `Trigger`, повиснет на mutex.

## Подсказка 3

`timer.Stop()` не поможет, если таймер уже сработал и его горутина ждёт mutex. Нумеруйте таймеры:

```go
d.gen++
gen := d.gen
d.timer = time.AfterFunc(at.Sub(now), func() {
	d.mu.Lock()
	if gen != d.gen || !d.pending { // устарел или отменён Stop
		d.mu.Unlock()
		return
	}
	d.pending = false
	d.mu.Unlock()
	d.f()
})
```

`Stop` под mutex увеличивает `gen`, сбрасывает `pending`, ставит `stopped` и возвращает, было ли что отменять.
