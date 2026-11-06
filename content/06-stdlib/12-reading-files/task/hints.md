## Подсказка 1

Читайте построчно через `bufio.NewScanner(r)` и считайте номер строки сами — увеличивайте счётчик на каждой итерации, включая пропущенные строки. `\r` в конце строки сканер уже убирает.

## Подсказка 2

Лимит строки по умолчанию — 64 КБ. Вызовите `sc.Buffer(make([]byte, 0, 64*1024), 2<<20)` до первого `Scan`. Делить строку по первому `=` удобно через `strings.Cut`. После цикла обязательно проверьте `sc.Err()` и оберните её через `%w`.

## Подсказка 3

```go
key, value, ok := strings.Cut(line, "=")
key = strings.TrimSpace(key)
if !ok || key == "" {
	return nil, fmt.Errorf("line %d: ожидается ключ = значение", n)
}
cfg[key] = strings.TrimSpace(value)
```

В `Load`: `f, err := os.Open(path)`, при ошибке верните её как есть (в ней уже есть `fs.ErrNotExist`), затем `defer f.Close()` и `return Parse(f)`.
