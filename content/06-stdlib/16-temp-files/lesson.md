---
title: Временные файлы
summary: os.CreateTemp и os.MkdirTemp, шаблон имени со звёздочкой, удаление через defer и t.TempDir в тестах.
difficulty: 2
minutes: 15
prerequisites:
  - 06-stdlib/13-writing-files
  - 06-stdlib/15-directories
docs:
  - title: "Пакет os: CreateTemp"
    url: https://pkg.go.dev/os#CreateTemp
  - title: "Пакет os: MkdirTemp"
    url: https://pkg.go.dev/os#MkdirTemp
  - title: "Пакет testing: T.TempDir"
    url: https://pkg.go.dev/testing#T.TempDir
---

`os.CreateTemp(dir, pattern)` создаёт и открывает файл с уникальным именем, `os.MkdirTemp(dir, pattern)` — каталог. Пустой `dir` означает `os.TempDir()`. В шаблоне последняя `*` заменяется случайной строкой: `"upload-*.csv"` → `upload-2741095386.csv`; без звёздочки случайная часть дописывается в конец.

Уникальность гарантирована: файл создаётся с флагом `O_EXCL`, и при совпадении имени функция пробует другое. Права — `0600` для файла и `0700` для каталога. А вот удаление — на вашей совести: временный файл живёт, пока его не удалят, и после завершения программы тоже.

## Как это в C#

- `Path.GetTempFileName()` создаёт пустой файл `tmpXXXX.tmp` без возможности задать имя и возвращает только путь. `os.CreateTemp` принимает шаблон, создаёт файл и сразу возвращает открытый `*os.File`.
- `Directory.CreateTempSubdirectory(prefix)` (.NET 7+) → `os.MkdirTemp`.
- `FileOptions.DeleteOnClose` удаляет файл при закрытии. В Go такого флага нет: пишут `defer os.Remove(f.Name())`.
- В xUnit/NUnit временные каталоги для тестов создают и чистят сами. В Go есть `t.TempDir()`: каталог уникален для теста и удаляется автоматически после его завершения.

## Ловушки

### Порядок `Close` и `Remove`

```go
f, _ := os.CreateTemp("", "x-*")
defer f.Close()
defer os.Remove(f.Name()) // выполнится первым — файл ещё открыт
```

`defer` выполняются в обратном порядке. В Unix удаление открытого файла работает, в Windows — нет: файл останется. Пишите `defer os.Remove(...)` **до** `defer f.Close()`, тогда сначала закроется, потом удалится.

### `os.Exit` и `log.Fatal` не выполняют `defer`

```go
dir, _ := os.MkdirTemp("", "work-")
defer os.RemoveAll(dir)
if err := run(dir); err != nil {
	log.Fatal(err) // os.Exit(1): RemoveAll не выполнится, каталог останется
}
```

Выносите работу в функцию, которая возвращает ошибку, а `os.Exit` вызывайте только в `main` после того, как отработали все `defer`.

### Временный файл как «быстрый» путь для атомарной записи

```go
tmp, _ := os.CreateTemp("", "cfg-*") // /tmp — часто другая файловая система
os.Rename(tmp.Name(), "/etc/app/config.json") // invalid cross-device link
```

Для атомарной замены временный файл создают в каталоге целевого: `os.CreateTemp(filepath.Dir(target), ...)`.

## Задание

Напишите две функции:

```go
func WithTempDir(prefix string, fn func(dir string) error) error
func Spool(r io.Reader, dir string) (string, error)
```

`WithTempDir` создаёт временный каталог в `os.TempDir()`, имя которого начинается с `prefix`, вызывает `fn` с путём к нему и удаляет каталог со всем содержимым:

- каталог удаляется всегда — и когда `fn` вернула ошибку, и когда `fn` запаниковала (паника после уборки летит дальше);
- если `fn` вернула ошибку, `WithTempDir` возвращает её (так, чтобы `errors.Is` её находил); если `fn` прошла успешно, а удаление не удалось — ошибку удаления; иначе `nil`;
- если каталог создать не удалось (например, `prefix` содержит `/`), `fn` не вызывается и возвращается ошибка.

`Spool` копирует всё содержимое `r` во временный файл в каталоге `dir` с именем вида `spool-*.tmp` и возвращает путь к нему:

- файл закрыт к моменту возврата, его содержимое совпадает с прочитанным из `r`;
- при ошибке чтения, записи или закрытия временный файл удаляется, возвращаются `""` и ошибка, для которой `errors.Is` находит исходную.
