---
title: Основы тестирования
summary: go test и файлы _test.go, TestXxx(t *testing.T), Errorf против Fatalf, Log и Skip, внутренний и внешний тестовый пакет, -run, -count=1 и кеш результатов.
difficulty: 2
minutes: 25
prerequisites:
  - 01-start/06-tooling
  - 04-errors/02-sentinel-errors
docs:
  - title: "Пакет testing"
    url: https://pkg.go.dev/testing
  - title: "go test: флаги и кеш (go help test, go help testflag)"
    url: https://pkg.go.dev/cmd/go#hdr-Test_packages
  - title: "Go Wiki: TestComments"
    url: https://go.dev/wiki/TestComments
---

В Go нет тестового фреймворка, тестового проекта и библиотеки ассертов — есть пакет `testing` и команда `go test`. Тесты лежат в том же каталоге, что и код, в файлах с суффиксом `_test.go`: обычная сборка (`go build`) их игнорирует, а `go test` компилирует пакет вместе с ними, генерирует `main`, который вызывает каждую функцию `TestXxx(t *testing.T)`, и запускает получившийся бинарник.

Проверка — обычный `if`, а провал — вызов метода `t`: `t.Errorf` отмечает тест упавшим и даёт ему продолжить, `t.Fatalf` отмечает и сразу останавливает. Сообщение принято писать в виде `Discount(1000, 15) = 900, want 850`: что вызвали, что получили, что ждали.

## Как это в C#

- **Тесты рядом с кодом.** В .NET тесты — отдельный проект `Shop.Tests` со ссылкой на `Shop` и NuGet-пакетами xUnit/NUnit, раннером и адаптером. В Go `cart_test.go` лежит рядом с `cart.go`, а всё нужное — в стандартной библиотеке и команде `go test`.
- **Имена вместо атрибутов.** `[Fact]`/`[Test]` не нужны: тестом считается функция `func TestXxx(t *testing.T)`, где после `Test` идёт не строчная буква (`TestSum`, `Test_sum`, но не `Testsum`). Нет и тестовых классов с конструктором и `IDisposable` — подготовка пишется прямо в тесте или в хелпере (урок о хелперах и фикстурах).
- **Нет ассертов.** Вместо `Assert.Equal(850, got)` — `if got != 850 { t.Errorf(...) }`. Многословнее, но сообщение пишете вы, и оно говорит, что именно сломалось. `Assert.*` бросает исключение и обрывает тест — в Go это поведение `t.Fatal`, а по умолчанию тест продолжается после `t.Error`.
- **Доступ к внутренностям.** Вместо `[InternalsVisibleTo]` — выбор пакета в тестовом файле: `package shop` видит всё, `package shop_test` — только экспортированное, как внешний потребитель.

Фильтр `-run` — регулярное выражение, без якорей: `go test -run Discount` запустит и `TestDiscount`, и `TestDiscountBadPercent`, а `-run 'Discount$'` — только первый. Результаты успешных прогонов `go test ./...` кешируются: при неизменных исходниках, флагах, а также переменных окружения и файлах, которые читал тест, команда печатает `ok shop (cached)` без запуска. `-count=1` отключает кеш — так запускают тесты, зависящие от внешнего мира. Перед запуском `go test` выполняет часть проверок `go vet` (printf, tests и ещё несколько): ошибка vet — это провал сборки тестов.

## Ловушки

### `Testsum` — не тест

```go
func Testsum(t *testing.T) { ... } // после Test — строчная буква
```

Такую функцию `go test` не считает тестом — раньше она молча не запускалась. Начиная с Go 1.24 анализатор `tests` из `go vet` входит в набор, который `go test` прогоняет сам, и сборка тестов падает: `Testsum has malformed name: first letter after 'Test' must not be lowercase`. А на `func TestTotal(t testing.T)` (без указателя) `go test` сам отвечает `wrong signature for TestTotal`.

### `t.Fatal` из горутины

```go
go func() {
	defer wg.Done()
	if err := work(); err != nil {
		t.Fatal(err) // останавливает только эту горутину
	}
}()
wg.Wait()
```

`Fatal`/`FailNow` завершают вызвавшую горутину через `runtime.Goexit`. Из чужой горутины тест помечается упавшим, но продолжает выполняться — а если горутина переживёт тест, будет паника. В горутинах используйте `t.Error` (он безопасен для конкурентного вызова) или передавайте ошибку в тест по каналу. Полный `go vet` (анализатор `testinggoroutine`) такой вызов находит, `go test` — нет.

### Зелёный тест из кеша

```
$ go test ./...
ok   example.com/shop  (cached)
```

Кеш учитывает исходники, флаги, а также переменные окружения и файлы, которые тест читал через `os`. Но не базу данных, не сеть и не внешние процессы: интеграционный тест может «пройти» из кеша, хотя сервис уже сломан. Для таких прогонов — `go test -count=1 ./...`, а в CI кеш и так обычно холодный.

## Задание

Напишите три проверки, которые получают `testing.TB` (интерфейс, общий для `*testing.T`, `*testing.B` и `*testing.F`) и функцию скидки `discount func(price, percent int) (int, error)`:

```go
func RequireEnv(tb testing.TB, key string) string
func CheckDiscount(tb testing.TB, discount func(int, int) (int, error), price, percent, want int)
func CheckDiscountError(tb testing.TB, discount func(int, int) (int, error), price, percent int, target error)
```

Требования:

- `RequireEnv` возвращает значение переменной окружения `key`. Если переменная не задана или пуста, тест пропускается: `tb.Skipf("%s не задана", key)`;
- `CheckDiscount` вызывает `discount(price, percent)` один раз. Если вернулась ошибка — `tb.Fatalf("Discount(%d, %d): неожиданная ошибка: %v", price, percent, err)`: продолжать проверку бессмысленно. Иначе, если результат не равен `want`, — `tb.Errorf("Discount(%d, %d) = %d, want %d", price, percent, got, want)`. Если всё совпало, никаких методов `tb` вызывать не нужно;
- `CheckDiscountError` тоже вызывает `discount` один раз и ждёт ошибку, совместимую с `target` по `errors.Is`. Ошибки нет — `tb.Errorf("Discount(%d, %d) = %d, nil; want error %v", price, percent, got, target)`; ошибка есть, но не та — `tb.Errorf("Discount(%d, %d): err = %v, want %v", price, percent, err, target)`;
- каждая проверка сообщает о провале ровно одним вызовом; используйте ровно те методы и форматы, что указаны выше (`Skipf`, `Fatalf`, `Errorf`) — тесты подставляют свой `testing.TB` и сверяют вызовы и тексты. `tb.Helper()` вызывать можно и полезно, он не считается сообщением о провале. Помните, что после `Fatalf` и `Skipf` код функции не продолжается: настоящий `testing.TB` (и фейк в тестах) завершает горутину через `runtime.Goexit`.
