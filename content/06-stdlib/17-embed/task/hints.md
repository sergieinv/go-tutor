## Подсказка 1

Проверьте `lang` до того, как строить путь: пройдите по символам и убедитесь, что каждый — `'a'..'z'` или `'-'`. Путь в `fs.FS` склеивайте через `path.Join("greetings", lang+".txt")` — пути `io/fs` всегда со слешами.

## Подсказка 2

`fs.ReadFile(fsys, name)` читает файл из любой `fs.FS`. Отличить «файла нет» от прочих ошибок помогает `errors.Is(err, fs.ErrNotExist)` — только в этом случае переходите на `greetings/en.txt`. Для `Languages` подойдёт `fs.Glob(fsys, "greetings/*.txt")` или `fs.ReadDir(fsys, "greetings")` с фильтрацией.

## Подсказка 3

```go
data, err := fs.ReadFile(fsys, path.Join("greetings", lang+".txt"))
if errors.Is(err, fs.ErrNotExist) {
	data, err = fs.ReadFile(fsys, "greetings/en.txt")
}
if err != nil {
	return "", err
}
return strings.TrimSpace(string(data)), nil
```

В `Languages` отсутствие каталога `greetings` — не ошибка: `fs.Glob` в этом случае просто вернёт пустой список. Имя без расширения — `strings.TrimSuffix(path.Base(m), ".txt")`; пропустите имена, которые не прошли бы проверку кода в `Greeting` (например, пустое у `greetings/.txt`), затем `slices.Sort`.
