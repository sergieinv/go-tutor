---
title: Пути к файлам
summary: path/filepath против path, Join, Base, Dir, Ext, Rel, Abs, Clean, Match и Glob, разделитель ОС и защита от выхода за пределы каталога.
difficulty: 2
minutes: 15
prerequisites:
  - 02-basics/10-strings-package
  - 06-stdlib/12-reading-files
docs:
  - title: "Пакет path/filepath"
    url: https://pkg.go.dev/path/filepath
  - title: "Пакет path"
    url: https://pkg.go.dev/path
---

Путь в Go — обычная `string`, отдельного типа нет. Операции над ним лежат в двух пакетах с одинаковыми функциями: `path/filepath` работает с путями текущей ОС (разделитель `\` в Windows, `/` в остальных), `path` — всегда со слешами. Для файлов на диске берите `filepath`, для URL, путей в архивах и в `io/fs` — `path`.

Функции чисто строковые: `Join`, `Dir`, `Base`, `Ext`, `Clean`, `Rel` не обращаются к диску и не проверяют, что файл существует. Исключения — `Abs` (читает текущий каталог), `Glob`, `EvalSymlinks` и обход каталогов.

## Как это в C#

- `System.IO.Path` — один класс на всё. В Go пакетов два, и выбор между ними — это выбор между «путь ОС» и «путь со слешами».
- `Path.Combine("a", "/b")` вернёт `/b` — абсолютный второй аргумент отбрасывает первый. `filepath.Join("a", "/b")` вернёт `a/b`: элементы просто склеиваются — как `Path.Join` в .NET Core 2.1+, только `Join` ещё и чистит путь (`Clean`).
- `Path.GetExtension` → `filepath.Ext`, `GetFileName` → `Base`, `GetDirectoryName` → `Dir`, `GetFileNameWithoutExtension` → `strings.TrimSuffix(base, filepath.Ext(base))`, `GetRelativePath` → `Rel`, `GetFullPath` → `Abs`.
- `Directory.GetFiles(dir, "*.log")` → `filepath.Glob(filepath.Join(dir, "*.log"))`. Шаблоны Go не рекурсивны: `**` не поддерживается.

## Ловушки

### `Join` не защищает от `..`

```go
// name пришёл из HTTP-запроса
p := filepath.Join("/srv/files", name) // name = "../../etc/passwd"
fmt.Println(p)                         // /etc/passwd
```

`Join` нормализует путь и честно поднимается вверх. Проверяйте пользовательский ввод через `filepath.IsLocal(name)` до склейки. С Go 1.24 есть и `os.OpenInRoot`/`os.Root` — открытие файлов, которое не выпускает за пределы каталога даже через символические ссылки.

### `filepath` для URL

```go
u := "https://example.com/" + filepath.Join("api", "users") // в Windows: api\users
```

На Linux ошибку не видно, на Windows URL ломается. Для URL — `path.Join` или `url.JoinPath`.

### `Ext` и файлы, начинающиеся с точки

```go
filepath.Ext("archive.tar.gz") // ".gz", а не ".tar.gz"
filepath.Ext(".bashrc")        // ".bashrc" — всё имя считается расширением
```

`Ext` смотрит только на последнюю точку последнего элемента. Двойные расширения и «скрытые» файлы Unix обрабатывайте сами.

## Задание

Напишите две функции:

```go
var ErrUnsafePath = errors.New("unsafe path")

func SafeJoin(base, name string) (string, error)
func ReplaceExt(p, ext string) string
```

`SafeJoin` присоединяет к каталогу `base` относительный путь `name`, пришедший извне в форме со слешами (например, из URL):

- если `name` пустой, абсолютный или после нормализации выходит за пределы `base` (`../x`, `a/../../x`), возвращается `""` и ошибка, для которой `errors.Is(err, ErrUnsafePath)`;
- иначе возвращается `filepath.Join(base, name)` (с переводом слешей в разделитель ОС); `..`, не выходящие наружу, допустимы: `SafeJoin("data", "a/../b.txt")` → `data/b.txt`, `SafeJoin("data", ".")` → `data`.

`ReplaceExt` заменяет расширение последнего элемента пути `p` на `ext` (`ext` либо пустой — тогда расширение просто удаляется, — либо начинается с точки):

- расширение — часть последнего элемента от последней точки, как у `filepath.Ext`: `report.tar.gz` + `.zip` → `report.tar.zip`;
- **исключение**: если последний элемент начинается с точки и других точек в нём нет (`.bashrc`), расширения у него нет — `ext` дописывается: `.bashrc` + `.bak` → `.bashrc.bak`;
- элемент без расширения получает `ext`: `bin/tool` + `.exe` → `bin/tool.exe`; точки в каталогах не учитываются: `v1.2/readme` + `.md` → `v1.2/readme.md`;
- остальная часть пути не меняется и не нормализуется: `./a//b.txt` + `.md` → `./a//b.md`.
