---
title: Разбор URL
summary: net/url — Parse и поля URL, Hostname и Port, Query и url.Values, экранирование QueryEscape/PathEscape, JoinPath и ResolveReference.
difficulty: 2
minutes: 20
prerequisites:
  - 02-basics/07-maps
  - 04-errors/01-errors
docs:
  - title: "Пакет net/url"
    url: https://pkg.go.dev/net/url
---

`url.Parse` разбирает строку в структуру `*url.URL` с полями `Scheme`, `User`, `Host`, `Path`, `RawQuery`, `Fragment`. Обратно её собирает метод `String()` — и сам экранирует путь. Это изменяемая структура: поменяли поле — получили другой URL.

Параметры запроса — тип `url.Values`, то есть `map[string][]string`: один ключ может встречаться несколько раз (`?tag=a&tag=b`). `u.Query()` разбирает `RawQuery` в новую map; чтобы изменения попали в URL, нужно присвоить обратно `u.RawQuery = q.Encode()`.

## Как это в C#

- `new Uri(s)` бросает `UriFormatException`; `url.Parse` возвращает ошибку. Но он намного терпимее: относительные ссылки, строки без схемы — всё это «корректные URL» с пустыми полями.
- `UriBuilder` → литерал `url.URL{Scheme: ..., Host: ..., Path: ...}` и `String()`. `uri.Host`/`uri.Port` → `u.Hostname()`/`u.Port()`; поле `u.Host` — хост вместе с портом. Порт по умолчанию Go не подставляет: у `https://ex.com` `Port()` — пустая строка.
- `HttpUtility.ParseQueryString` / `QueryHelpers` → `u.Query()` и `url.Values` с `Get`, `Set`, `Add`, `Del`, `Has`, `Encode`.
- `Uri.EscapeDataString` → `url.PathEscape` (пробел → `%20`) или `url.QueryEscape` (пробел → `+`); `new Uri(base, relative)` → `base.ResolveReference(ref)`.

## Ловушки

### Строка без схемы

```go
u, err := url.Parse("example.com/path")
fmt.Println(u.Host == "", u.Path, err) // true example.com/path <nil>

u, _ = url.Parse("localhost:8080") // Scheme == "localhost", Opaque == "8080"
```

Ошибки нет: без `//` хоста нет, а всё до первого `:` — схема. Проверяйте `u.Scheme` и `u.Host` явно, если ждёте абсолютный URL.

### `Query()` возвращает копию

```go
u.Query().Set("page", "2") // изменили временную map
fmt.Println(u)             // page не появился
```

Каждый вызов `Query()` заново разбирает `RawQuery`. Сохраните map в переменную, измените и запишите `u.RawQuery = q.Encode()`.

### Сборка URL конкатенацией

```go
s := "https://api.example.com/search?q=" + query // query = "a&admin=1"
```

Пользовательский ввод добавит свой параметр. Значения экранируйте `url.QueryEscape` или собирайте через `url.Values{...}.Encode()`, сегменты пути — `url.PathEscape` или `url.JoinPath`.

## Задание

Напишите функцию `Normalize(raw string) (string, error)`, которая приводит HTTP-адрес к каноническому виду — например, чтобы использовать как ключ кеша.

Требования:

- `raw` разбирается `url.Parse`; ошибка разбора — ошибка;
- схема — только `http` или `https` в любом регистре, иначе ошибка; хост не может быть пустым, иначе ошибка;
- схема и имя хоста приводятся к нижнему регистру;
- порт по умолчанию (`80` для `http`, `443` для `https`) удаляется, любой другой — сохраняется;
- пустой путь заменяется на `/`, непустой не меняется;
- параметры запроса сортируются по ключу, а значения одного ключа сохраняют исходный порядок — ровно как делает `url.Values.Encode`; пустой запрос (в том числе одиночный `?`) — без `?`;
- фрагмент `#...` и данные пользователя `user:pass@` удаляются;
- результат — `String()` получившегося URL.

Примеры: `HTTP://Example.COM:80?b=2&a=1#top` → `http://example.com/?a=1&b=2`; `https://ex.com:8443/a%20b` → `https://ex.com:8443/a%20b`.
