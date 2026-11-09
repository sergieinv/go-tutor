## Подсказка 1

Коллекции дескрипторов обходят по индексу: `for i := range fd.Messages().Len() { m := fd.Messages().Get(i) ... }`. Вложенность рекурсивная: у сообщения есть `Fields()`, `Enums()` и `Messages()` — вложенные сообщения, у которых снова то же самое. Enum бывают на верхнем уровне (`fd.Enums()`) и внутри сообщений (`m.Enums()`) — проверьте оба места одной функцией.

## Подсказка 2

Имя поля проверяет регулярное выражение `^[a-z][a-z0-9]*(_[a-z0-9]+)*$` (`regexp.MustCompile` один раз на уровне пакета), полное имя — `f.FullName()`. Первое значение enum — `e.Values().Get(0)`, проверка — `strings.HasSuffix(string(v.Name()), "_UNSPECIFIED")`, а в `Element` идёт `v.FullName()` (имя enum в нём не участвует — так устроены области видимости protobuf).

## Подсказка 3

Методы: `fd.Services().Get(i).Methods().Get(j)`. Допустимые имена запроса — `string(md.Name())+"Request"` и `string(sd.Name())+string(md.Name())+"Request"`; сравнивайте с `md.Input().Name()` (короткое имя, без пакета), ответ — так же с `md.Output().Name()`. В конце отсортируйте: `slices.SortFunc(out, func(a, b Issue) int { return cmp.Or(strings.Compare(string(a.Element), string(b.Element)), strings.Compare(a.Rule, b.Rule)) })`.
