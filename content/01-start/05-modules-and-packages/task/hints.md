## Подсказка 1

В `IsExported` нельзя смотреть на `name[0]`: это первый **байт**, а «Ё» в UTF-8 занимает два. Первую руну даёт `utf8.DecodeRuneInString(name)`, для пустой строки она вернёт `utf8.RuneError`, который заглавной буквой не считается.

## Подсказка 2

В `CanImport` сначала найдите родителя последнего `internal`. Элемент целиком — это `/internal/` в середине пути или `/internal` в конце: так `internals` и `myinternal` не совпадут. Пригодятся `strings.LastIndex` и `strings.HasSuffix`.

## Подсказка 3

Если `i := strings.LastIndex(imported, "/internal/")` не меньше нуля, родитель — `imported[:i]`; иначе проверьте суффикс `/internal`; иначе импорт разрешён. Затем: `importer == parent || strings.HasPrefix(importer, parent+"/")` — именно с `/`, иначе `example.com/shop2` пройдёт как потомок `example.com/shop`.
