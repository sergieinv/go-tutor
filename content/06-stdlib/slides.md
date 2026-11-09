# Стандартная библиотека

Обзор модуля для показа команде: что в Go закрывает стандартная библиотека — то, за чем в .NET идут в BCL или NuGet.

---

## Сортировка без LINQ

```go
slices.SortFunc(users, func(a, b User) int {
	return cmp.Or(
		cmp.Compare(a.City, b.City), // OrderBy(u => u.City)
		cmp.Compare(b.Age, a.Age),   // ThenByDescending(u => u.Age)
	)
})
```

- Сортирует **на месте**, как `List<T>.Sort()`; нужна копия — `slices.Clone`.
- `SortFunc` неустойчив, `SortStableFunc` устойчив.
- Сравнивайте через `cmp.Compare`, а не вычитанием: `a - b` переполняется.

---

## Форматирование вместо интерполяции

| C# | Go |
|---|---|
| `$"{name}: {total:F2}"` | `fmt.Sprintf("%s: %.2f", name, total)` |
| `{s,-10}`, `{x,8:F3}` | `%-10s`, `%8.3f` |
| `ToString()` | `String() string` (`fmt.Stringer`) |
| `FormatException` | вывод `%!d(string=x)` — ловит `go vet` |

`%v` печатает что угодно, включая структуры, срезы и map. `String()` на указателе не вызывается для значения.

---

## JSON: теги вместо атрибутов

```go
type Order struct {
	ID    int64     `json:"id"`
	Note  string    `json:"note,omitempty"`
	At    time.Time `json:"at,omitzero"` // Go 1.24+
	Debug string    `json:"-"`
	total int       // с маленькой буквы — не видно json
}
```

`[JsonPropertyName]` → `json:"name"`, `[JsonIgnore]` → `json:"-"`, `JsonConverter<T>` → методы `MarshalJSON`/`UnmarshalJSON` на типе. Теги — строки: опечатку найдёт `go vet` или тест.

---

## JSON и XML: тихие сюрпризы

- Неэкспортируемые поля молча пропадают — ни ошибки, ни предупреждения.
- `omitempty` не пропускает структуры (`time.Time`); для них `omitzero`.
- Числа в `map[string]any` — всегда `float64`: точно только до 2⁵³. Нужна точность — структура с `int64` или `Decoder.UseNumber()`.
- Ключи читаются без учёта регистра; строгость — `Decoder.DisallowUnknownFields()`.
- XML — те же правила плюс `,attr`, `,chardata` и пути `a>b`.

---

## Время: `time.Time` и `Duration`

| C# | Go |
|---|---|
| `DateTimeOffset` | `time.Time` — момент + `*time.Location` |
| `TimeSpan`, `Stopwatch` | `time.Duration` (наносекунды), `time.Since(start)` |
| `a == b` | `a.Equal(b)`: `==` сравнивает и пояс |
| `DateTime?` = `null` | нулевое значение, проверка `t.IsZero()` |

`Truncate(24 * time.Hour)` считает от UTC и не даёт полночь в поясе.

---

## Формат времени — эталонная дата

Раскладку пишут так, как выглядело бы 2 января 2006 года, 15:04:05, пояс MST (−07:00): месяц 1, день 2, час 3 (15), минута 4, секунда 5, год 6, пояс −7.

```go
t.Format("2006-01-02 15:04") // вместо ToString("yyyy-MM-dd HH:mm")
t.Format("yyyy-MM-dd")       // "yyyy-MM-dd" — ошибки не будет

loc, _ := time.LoadLocation("Europe/Moscow")
t, err := time.ParseInLocation("2006-01-02 15:04", s, loc) // Parse без смещения — UTC
```

---

## Разбор входных строк — без исключений

- `strconv.ParseInt(s, 10, 8)` — один разбор на все размеры; ошибка `*strconv.NumError` с `ErrSyntax` или `ErrRange`, при переполнении возвращает граничное значение.
- `regexp` — движок RE2: линейное время на любом вводе, но без lookaround и `\1`. Компилируют один раз в переменную пакета.
- `url.Parse("localhost:8080")` ошибки не даёт; `u.Query()` — копия, изменения записывают в `u.RawQuery`.
- Подписи сравнивают `hmac.Equal`, а не `bytes.Equal`; пароли хешируют bcrypt/argon2id, не SHA-256.

---

## Файлы через `io.Reader` и `io.Writer`

| C# | Go |
|---|---|
| `File.ReadAllBytes` / `WriteAllBytes` | `os.ReadFile` / `os.WriteFile` |
| `StreamReader.ReadLine()` | `bufio.Scanner` — лимит строки 64 КБ |
| `StreamWriter` + `Dispose` | `bufio.Writer` + явный `Flush` |
| `File.Replace` | временный файл в том же каталоге + `os.Rename` |

- После цикла `Scan` проверяют `sc.Err()`: обрыв выглядит как конец файла.
- При записи ошибку `Close` не теряют — `defer f.Close()` её проглатывает.

---

## Пути, каталоги, `embed`

- `path/filepath` — пути ОС, `path` — всегда со слешами (URL, `io/fs`, архивы).
- `filepath.Join` не защищает от `..`: пользовательский ввод проверяют `filepath.IsLocal` или открывают через `os.Root`.
- `fs.FS` — файловая система как интерфейс: диск, `embed.FS`, zip, `fstest.MapFS` в тестах.

```go
//go:embed templates
var templates embed.FS // нет файла — ошибка компиляции, а не рантайма
```

---

## Утилита командной строки

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1) // defer внутри run уже выполнились
	}
}
```

- Флаги — `flag.NewFlagSet(name, flag.ContinueOnError)`; окружение — `os.LookupEnv`, чтобы отличить пустое от незаданного.
- Логи — `slog.Info("вход", "user_id", id)`; процессы — `exec.Command("ls", "-la", dir)` без оболочки.

---

## Что запомнить

- `slices.Sort` и `bufio.Writer` работают на месте и в буфере — копия и `Flush` за вами.
- JSON видит только экспортируемые поля; числа в `any` — `float64`.
- `time.Time` сравнивают `Equal`, форматируют от эталона `2006-01-02 15:04:05`.
- Ошибку `Close` при записи и `sc.Err()` после чтения проверяют всегда.
- `os.Exit` и `log.Fatal` не выполняют `defer` — вызывайте их только в `main`.
