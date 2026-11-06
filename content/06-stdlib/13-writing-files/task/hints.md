## Подсказка 1

Временный файл: `os.CreateTemp(filepath.Dir(path), "имя-*")` — звёздочка заменится случайной строкой. Сделайте результат функции именованным `(err error)` и в `defer` удаляйте временный файл, если `err != nil`, — так уборка сработает на любом из ранних `return`.

## Подсказка 2

Оберните файл в `bufio.NewWriter(tmp)` и передайте в `write` именно буферизованный writer. После `write` порядок такой: `Flush` → `Close` → `Chmod(…, 0o644)` → `Rename`. Каждую ошибку проверяйте: `Flush` и `Close` — места, где проявляются сбои записи.

## Подсказка 3

```go
defer func() {
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
	}
}()
w := bufio.NewWriter(tmp)
if err := write(w); err != nil {
	return err
}
if err := w.Flush(); err != nil {
	return err
}
```

Дальше `tmp.Close()`, `os.Chmod(tmp.Name(), 0o644)` и `return os.Rename(tmp.Name(), path)`.
