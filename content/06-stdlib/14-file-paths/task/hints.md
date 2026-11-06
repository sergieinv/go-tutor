## Подсказка 1

Не проверяйте результат `Join` через `strings.HasPrefix(p, base)`: для `base = "data"` путь `data2/x` тоже начинается с `data`. В `path/filepath` есть готовая проверка относительного пути — `filepath.IsLocal`. Перед ней переведите слеши в разделитель ОС: `filepath.FromSlash(name)`.

## Подсказка 2

Для `ReplaceExt` расширение даёт `filepath.Ext(p)` — оно уже учитывает только последний элемент пути. Отрежьте его с конца строки через `strings.TrimSuffix` и допишьте `ext`. Остаётся случай `.bashrc`: когда `Ext` возвращает весь последний элемент целиком.

## Подсказка 3

```go
old := filepath.Ext(p)
if old == filepath.Base(p) { // ".bashrc" — не расширение
	old = ""
}
return strings.TrimSuffix(p, old) + ext
```

В `SafeJoin`: `if !filepath.IsLocal(local) { return "", fmt.Errorf("%w: %q", ErrUnsafePath, name) }`, иначе `filepath.Join(base, local)`.
