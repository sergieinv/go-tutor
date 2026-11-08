## Подсказка 1

Пройдите по разделам чек-листа «Ошибки», «context и отмена», «Ресурсы и горутины» и для каждой функции спросите: что она делает с ошибкой (возвращает, оборачивает, глотает, пишет в лог), какой контекст передаёт дальше, что будет при неверном вводе и кто остановит горутину, которую она запускает. Замечаний шесть, сигнатуры менять не нужно.

## Подсказка 2

Подозрительные места: `panic` в `Price`; `context.Background()` при вызове источника; `log.Printf` рядом с `return`; `%v` и текст `"Failed to get price: ..."`; `continue` после ошибки в `Total`; `Close`, который только останавливает тикер. Помните: `Ticker.Stop` не закрывает канал `C`, так что `for range c.ticker.C` после него ждёт вечно.

## Подсказка 3

- `Price`: пустой `sku` → `return 0, ErrInvalidSKU`; источнику — `ctx` вызова; ошибка → `return 0, fmt.Errorf("price %q: %w", sku, err)` без лога;
- `Total`: `if err != nil { return 0, fmt.Errorf("total: %w", err) }`;
- горутина: в `Catalog` поля `done chan struct{}` и `wg sync.WaitGroup`; в `NewCatalog` — `c.wg.Go(func() { c.loop(ttl) })`; в `loop` — `select` по `<-c.done` и `<-t.C` с `defer t.Stop()`; `Close` — `close(c.done)` и `c.wg.Wait()`.
