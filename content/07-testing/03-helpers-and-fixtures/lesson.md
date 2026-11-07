---
title: Хелперы и фикстуры
summary: t.Helper, t.Cleanup против defer, t.TempDir, t.Setenv, t.Chdir и t.Context, TestMain, golden-файлы в testdata и флаг -update, хелперы на testing.TB.
difficulty: 3
minutes: 30
prerequisites:
  - 07-testing/02-table-driven
  - 04-errors/05-defer
docs:
  - title: "Пакет testing: Main и Cleanup"
    url: https://pkg.go.dev/testing#hdr-Main
  - title: "Go Wiki: TestComments — хелперы и сообщения"
    url: https://go.dev/wiki/TestComments
  - title: "Пакет testing: type TB"
    url: https://pkg.go.dev/testing#TB
---

В Go нет `[SetUp]`, `[TearDown]` и fixture-классов. Подготовку пишут обычными функциями-хелперами, которые принимают `t` (или интерфейс `testing.TB`, общий для тестов, бенчмарков и fuzz-тестов), создают ресурс и сами регистрируют его освобождение через `t.Cleanup`. Тест читает как сценарий: `db := newTestDB(t)` — и всё, закрывать не нужно.

Первая строка хелпера — `t.Helper()`. Она помечает функцию как вспомогательную, и в сообщениях о провале указывается строка вызывающего теста, а не строка внутри хелпера: иначе все падения показывали бы на одну и ту же строку в `assertEqual`.

Для окружения у `testing.T` есть готовые фикстуры: `t.TempDir()` (каталог, удаляется после теста), `t.Setenv` (переменная окружения на время теста), `t.Chdir` и `t.Context()` (оба — Go 1.24: смена рабочего каталога и контекст, отменяемый перед Cleanup). Подготовка на весь пакет — функция `TestMain(m *testing.M)`. Большие ожидаемые результаты хранят в golden-файлах в `testdata/` и перезаписывают флагом `-update`.

## Как это в C#

- **Фикстуры.** `[SetUp]`/`[TearDown]`, конструктор и `IDisposable` тестового класса, `IClassFixture<T>` → хелпер `newX(t)` с `t.Cleanup` внутри. Нет наследования и общих полей класса — каждая зависимость видна в теле теста.
- **Общая подготовка на коллекцию или сборку** (`ICollectionFixture<T>`, `[AssemblyFixture]` в xUnit v3, `[SetUpFixture]` + `[OneTimeSetUp]` в NUnit) → `TestMain` на пакет. Нужен редко: обычно хватает ленивой инициализации через `sync.Once` в хелпере.
- **Snapshot-тесты** (Verify, ApprovalTests: файлы `*.verified.txt` и принятие изменений) → golden-файлы: эталон в `testdata/`, обновление — `go test -update`, проверка изменений — обычный `git diff`.
- **`[CallerLineNumber]` в своих ассертах** → `t.Helper()`: строку вызова `testing` находит сам, пропуская помеченные функции.

## Ловушки

### `defer` в хелпере вместо `t.Cleanup`

```go
func newStore(t *testing.T) *Store {
	t.Helper()
	s := Open(t.TempDir())
	defer s.Close() // закроется при выходе из newStore — тест получит закрытый Store
	return s
}
```

`defer` срабатывает при выходе из функции, где он написан, — из хелпера. `t.Cleanup(func() { s.Close() })` выполнится после теста и всех его подтестов, причём Cleanup-функции идут в обратном порядке регистрации, как `defer`.

### `os.Exit` в `TestMain` отменяет `defer`

```go
func TestMain(m *testing.M) {
	db := startDB()
	defer db.Stop()   // не выполнится
	os.Exit(m.Run())
}
```

`os.Exit` завершает процесс сразу, отложенные вызовы не выполняются. С Go 1.15 `os.Exit` не обязателен: если `TestMain` просто вернётся, код выхода возьмётся из `m.Run()`. Пишите `code := m.Run(); db.Stop(); os.Exit(code)` или обходитесь без `os.Exit` и оставьте `defer`.

### `t.Setenv` и `t.Parallel`

```go
func TestA(t *testing.T) {
	t.Parallel()
	t.Setenv("MODE", "test") // panic: test using t.Setenv or t.Chdir can not use t.Parallel
}
```

Переменные окружения и рабочий каталог — общие на весь процесс: параллельный тест увидел бы чужие значения. Поэтому `t.Setenv` и `t.Chdir` паникуют в параллельном тесте (и наоборот — `t.Parallel` после них). Если тест должен быть параллельным, передавайте настройки параметрами, а не через окружение.

## Задание

Напишите два хелпера на `testing.TB`:

```go
func Golden(tb testing.TB, path, got string, update bool)
func Swap[T any](tb testing.TB, p *T, v T)
```

`Golden` сравнивает `got` с содержимым golden-файла `path`:

- первым делом вызывает `tb.Helper()` — до любых других методов `tb`;
- если `update == true`, записывает `got` в `path` (недостающие каталоги создаёт с правами `0o755`, файл — `0o644`) и больше ничего не проверяет. Ошибку записи сообщает `tb.Fatalf("golden: %v", err)`;
- иначе читает файл; ошибку чтения сообщает `tb.Fatalf("golden: %v", err)`;
- если содержимое совпало с `got` — других вызовов `tb` нет. Иначе обе строки делятся на строки по `"\n"` (`strings.Split`), и находится первая строка с номером `N` (с 1), где они различаются или где одна из них уже закончилась. Сообщение — ровно один вызов `tb.Errorf("%s: строка %d:\n got: %s\nwant: %s", path, N, g, w)`, где `g` и `w` — строки `got` и эталона в виде `strconv.Quote(line)`, а отсутствующая строка выводится как `<EOF>` без кавычек. Например, `got = "a\nb"` против эталона `"a\nc\n"` — строка 2: `"b"` против `"c"`; `got = "a\n"` против `"a"` — строка 2: `""` против `<EOF>`.

`Swap` подменяет значение на время теста — так в тестах подменяют пакетные переменные вроде `var now = time.Now`:

- первым делом вызывает `tb.Helper()`;
- запоминает старое значение `*p`, присваивает `*p = v` и регистрирует через `tb.Cleanup` функцию, которая вернёт старое значение. Несколько `Swap` одной переменной после выполнения Cleanup-функций (в обратном порядке) возвращают исходное значение.

Тесты подставляют свой `testing.TB`, который записывает вызовы, поэтому используйте ровно указанные методы.
