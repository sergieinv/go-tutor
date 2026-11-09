# Тестирование и качество

Обзор модуля для показа команде: тесты, бенчмарки, fuzzing и моки на одном пакете `testing` — без фреймворков и тестовых проектов.

---

## Тест — функция рядом с кодом

```go
// cart_test.go — рядом с cart.go
func TestDiscount(t *testing.T) {
	if got := Discount(1000, 15); got != 850 {
		t.Errorf("Discount(1000, 15) = %d, want 850", got)
	}
}
```

| C# | Go |
|---|---|
| проект `Shop.Tests`, xUnit/NUnit | файл `_test.go` и команда `go test` |
| `[Fact]` | имя `TestXxx(t *testing.T)` |
| `Assert.Equal` бросает исключение | `if` + `t.Errorf` (тест продолжается) или `t.Fatalf` |
| `[InternalsVisibleTo]` | `package shop` изнутри или `package shop_test` снаружи |

---

## Табличные тесты и подтесты

```go
tests := []struct{ name, in, want string }{
	{"простой", "Hello World", "hello-world"},
	{"пробелы по краям", "  Go  ", "go"},
}
for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		if got := Slug(tt.in); got != tt.want {
			t.Errorf("Slug(%q) = %q, want %q", tt.in, got, tt.want)
		}
	})
}
```

Аналог `[Theory]` + `[InlineData]`, но в таблице — что угодно. Один случай: `go test -run 'TestSlug/пробелы_по_краям'`.

---

## Хелперы вместо `[SetUp]` и fixture-классов

- Хелпер принимает `t`, первой строкой зовёт `t.Helper()` и сам регистрирует уборку через `t.Cleanup`.
- Готовые фикстуры: `t.TempDir()`, `t.Setenv`, `t.Chdir`, `t.Context()`.
- `defer` в хелпере закроет ресурс при выходе из хелпера — до теста.
- Snapshot-тесты → golden-файлы в `testdata/`, обновление флагом `-update`.

| C# | Go |
|---|---|
| `[SetUp]` / `IDisposable` | `db := newTestDB(t)` с `t.Cleanup` внутри |
| `[OneTimeSetUp]`, `ICollectionFixture` | `TestMain(m *testing.M)` — редко |

---

## Параллельность и фейковое время

- Тесты идут последовательно; `t.Parallel()` — явное разрешение. Наоборот, чем в xUnit.
- Параллельные подтесты стартуют после возврата родителя: общие ресурсы — через `t.Cleanup`, не `defer`.
- `testing/synctest` (Go 1.25) — пузырь с фейковыми часами, код менять не нужно:

```go
synctest.Test(t, func(t *testing.T) {
	c := New(time.Minute)
	c.Set("k", "v")
	time.Sleep(time.Minute) // мгновенно: часы прыгают, когда все ждут
	if _, ok := c.Get("k"); ok {
		t.Fatal("запись жива после TTL")
	}
})
```

---

## Бенчмарки без BenchmarkDotNet

```go
func BenchmarkJoin(b *testing.B) {
	parts := makeParts(100) // подготовка один раз, вне замера
	for b.Loop() {          // Go 1.24+
		strings.Join(parts, ",")
	}
}
```

- `go test -bench=. -benchmem` → `ns/op`, `B/op`, `allocs/op`.
- Одному замеру не верят: `-count=10` и `benchstat old.txt new.txt`.
- `testing.AllocsPerRun` закрепляет число аллокаций обычным тестом.

---

## Fuzzing встроен в `go test`

```go
func FuzzRoundTrip(f *testing.F) {
	f.Add("aaab") // seed-корпус
	f.Fuzz(func(t *testing.T, s string) {
		dec, err := Decode(Encode(s))
		if err != nil || dec != s {
			t.Errorf("Decode(Encode(%q)) = %q, %v", s, dec, err)
		}
	})
}
```

- Проверяют свойства: обратимость, инварианты, совпадение с эталоном.
- Без флага — обычный регрессионный тест; `go test -fuzz=FuzzRoundTrip` — поиск, упавший вход ложится в `testdata/fuzz/` и коммитится.

---

## Примеры, покрытие, линтеры

- `func ExampleSplit()` с комментарием `// Output:` — и документация в `go doc`, и тест. Без `Output:` пример только компилируется.
- `go test -cover`, `-coverprofile=c.out`, `go tool cover -html=c.out`. Покрытие показывает непроверенное, а не проверенное.
- `go vet` — из поставки, часть проверок `go test` запускает сам. Остальное — golangci-lint с `.golangci.yml` (errcheck, staticcheck, revive, gosec…).

---

## Фейки и `httptest` вместо Moq

- Интерфейс объявляет потребитель, в нём один-два метода — фейк занимает пять строк.
- Время — тоже зависимость: `now func() time.Time`.

```go
rec := httptest.NewRecorder()
h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/convert?amount=10&cur=USD", nil))
if rec.Code != http.StatusOK {
	t.Errorf("code = %d, want 200", rec.Code)
}
```

Для клиента — настоящий сервер на loopback: `srv := httptest.NewServer(h)`, `srv.URL`, `t.Cleanup(srv.Close)`.

---

## Тесты в CI

```sh
test -z "$(gofmt -l .)"
go vet ./...
golangci-lint run
go test -race -shuffle=on -count=1 ./...  # без кеша, с гонками, в случайном порядке
go test -race -tags=integration ./...     # отдельный job с БД
```

- Флак — это баг, а не повод перезапустить: `go test -run TestX -count=200 -race`.
- Никакого `time.Sleep` для синхронизации: канал, `WaitGroup` или `synctest`.

---

## Что запомнить

- Сообщение о провале: `Discount(1000, 15) = 900, want 850` — вызов, затем got, затем want.
- `t.Fatal` останавливает только свою горутину — в горутинах `t.Error`.
- Уборка — `t.Cleanup`, а не `defer` в хелпере или родителе параллельных подтестов.
- Результат `go test` кешируется: для внешнего мира — `-count=1`.
- Проверяйте поведение, а не реализацию: фейк с состоянием лучше мока с ожиданиями.
