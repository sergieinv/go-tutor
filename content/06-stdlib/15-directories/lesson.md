---
title: Каталоги
summary: MkdirAll, ReadDir, обход дерева WalkDir и fs.SkipDir, RemoveAll и абстракция файловой системы io/fs с os.DirFS.
difficulty: 3
minutes: 20
prerequisites:
  - 06-stdlib/14-file-paths
  - 03-types/03-interfaces
docs:
  - title: "Пакет path/filepath: WalkDir"
    url: https://pkg.go.dev/path/filepath#WalkDir
  - title: "Пакет io/fs"
    url: https://pkg.go.dev/io/fs
---

`os.MkdirAll` создаёт цепочку каталогов, `os.ReadDir` читает один уровень, `filepath.WalkDir` обходит дерево целиком, `os.RemoveAll` удаляет его. Обход управляется возвращаемым значением колбэка: `nil` — идти дальше, `fs.SkipDir` — не заходить в этот каталог, `fs.SkipAll` — закончить обход, любая другая ошибка — прервать обход и вернуть её.

Пакет `io/fs` описывает файловую систему как интерфейс `fs.FS` с одним методом `Open`. Его реализуют `os.DirFS` (каталог на диске), `embed.FS` (файлы, вшитые в бинарник), `zip.Reader` и `fstest.MapFS` (файлы в памяти для тестов). Функции `fs.WalkDir`, `fs.ReadFile`, `fs.Glob` работают с любой из них.

## Как это в C#

- `Directory.CreateDirectory` уже создаёт всю цепочку — как `MkdirAll`. `os.Mkdir` создаёт ровно один уровень и падает, если родителя нет.
- `Directory.EnumerateFileSystemEntries(dir, "*", SearchOption.AllDirectories)` — плоский перечислитель; пропустить поддерево им нельзя, для этого нужен низкоуровневый `FileSystemEnumerable<T>` с `ShouldRecursePredicate`. В Go `WalkDir` с `fs.SkipDir` позволяет не заходить в `node_modules` или `.git`.
- `Directory.Delete(dir, recursive: true)` → `os.RemoveAll`. Но `RemoveAll` для несуществующего пути возвращает `nil`, а не исключение.
- Абстракции файловой системы в BCL нет: есть `IFileProvider` (Microsoft.Extensions.FileProviders, только чтение), а для тестов берут `System.IO.Abstractions` из NuGet. В Go `fs.FS` встроен и используется самой стандартной библиотекой (`template.ParseFS`, `http.FS`).

## Ловушки

### `SkipDir`, возвращённый для файла

```go
filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
	if d.Name() == "vendor" {
		return fs.SkipDir // а если vendor — файл?
	}
	return nil
})
```

Для каталога `SkipDir` пропускает его содержимое. Для **файла** — все оставшиеся файлы в его каталоге. Проверяйте `d.IsDir()`, прежде чем возвращать `SkipDir`.

### Параметр `err` в колбэке

```go
filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
	fmt.Println(d.Name()) // паника: при err != nil d может быть nil
	return nil
})
```

Если каталог не удалось прочитать, `WalkDir` вызывает колбэк с ненулевым `err`, а если не существует сам корень — ещё и с `d == nil`. Обработайте `err` первой строкой: верните её, чтобы прервать обход, или `nil`, чтобы пропустить проблемный каталог.

### Пути в `fs.FS` — не пути ОС

```go
fsys := os.DirFS("/srv/app")
fs.ReadFile(fsys, "/config.json")             // ошибка: ведущий слеш недопустим
fs.ReadFile(fsys, filepath.Join("a", "b.txt")) // в Windows — "a\\b.txt", тоже ошибка
```

Пути `io/fs` всегда со слешами, без ведущего `/`, без `..`, корень — `"."` (проверка — `fs.ValidPath`). Склеивайте их через `path.Join`, а не `filepath.Join`.

## Задание

Напишите поиск больших файлов в дереве:

```go
func FindLarge(fsys fs.FS, minSize int64, skipDirs []string) ([]string, error)
```

Требования:

- обходится всё дерево `fsys`, начиная с корня `"."`;
- результат — пути (в форме `fs.FS`, например `src/big.bin`) обычных файлов размером не меньше `minSize` байт; каталоги в результат не попадают;
- каталоги, **имя** которых есть в `skipDirs` (на любой глубине), пропускаются вместе со всем содержимым; файл с таким же именем не пропускается и проверяется как обычно; корень `"."` не пропускается никогда;
- пути в результате идут в лексикографическом порядке; если подходящих файлов нет — пустой срез (`nil` допустим) и `nil`-ошибка;
- если обход сообщил об ошибке (корень или каталог не открывается либо не читается) или `Info` записи файла вернул ошибку, `FindLarge` прерывает обход и возвращает `nil` и ошибку, для которой `errors.Is` находит исходную.
