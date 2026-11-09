## Подсказка 1

Кеш — `map[string]entry` под `sync.Mutex`, где `entry` хранит товар и момент загрузки; «свежая» — `now() < loadedAt + TTL`. Защиту от толпы даёт `singleflight.Group` из `golang.org/x/sync/singleflight`: все вызовы `Do`/`DoChan` с одним ключом, пока функция выполняется, ждут её и получают её результат — значение и ошибку. Мьютекс не держите во время загрузки: иначе загрузка одного ключа остановит все остальные.

## Подсказка 2

`Do` ждёт загрузку без оглядки на контекст. Чтобы ждущий мог уйти по отмене, а загрузка продолжилась для остальных, нужен `DoChan` и `select` по его каналу и `ctx.Done()`. Сама загрузка получает `context.WithoutCancel(ctx)` — значения сохраняются, отмена и дедлайн вызывающего нет; при `LoadTimeout > 0` оберните его ещё в `context.WithTimeout`.

## Подсказка 3

Внутри функции для `DoChan` сначала ещё раз проверьте свежесть: горутина могла увидеть промах до того, как предыдущая загрузка записала значение и `singleflight` забыл ключ.

```go
ch := c.g.DoChan(sku, func() (any, error) {
	if p, ok := c.fresh(sku); ok {
		return p, nil
	}
	p, err := c.load(loadCtx, sku)
	if err == nil {
		// записать entry{p, c.cfg.Now()} под мьютексом — момент конца загрузки
	}
	return p, err
})
select {
case r := <-ch:
	return r.Val.(Product), r.Err
case <-ctx.Done():
	return Product{}, ctx.Err()
}
```
