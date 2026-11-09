## Подсказка 1

Начните с проверки цели: `rv := reflect.ValueOf(dst)`; нужны `rv.Kind() == reflect.Pointer`, `!rv.IsNil()` и `rv.Elem().Kind() == reflect.Struct` (для `dst == nil` `Kind()` вернёт `Invalid`). Дальше `sv := rv.Elem()` — адресуемая структура, а `st := sv.Type()` даёт поля: `st.Field(i).Tag.Lookup("env")` и `sv.Field(i)` для значения.

## Подсказка 2

Тег разберите через `strings.Cut(tag, ",")`: слева имя, справа опции (допустима только `required`). Порядок проверок поля: тег корректен → `fv.CanSet()` (неэкспортируемое поле даст `false`) → тип поддерживается → есть ли переменная в `env`. Каждую ошибку оборачивайте в `&FieldError{Field, Key, Err}`; для `ErrUnsupported` с деталями — `fmt.Errorf("%w: ...", ErrUnsupported)`.

## Подсказка 3

Сначала сравните тип с `reflect.TypeFor[time.Duration]()` — у длительности `Kind()` равен `Int64`, и проверка по `Kind` отправит её в `strconv`. Затем `switch fv.Kind()`: `String` → `SetString`, `Int`/`Int64` → `strconv.ParseInt(raw, 10, fv.Type().Bits())` и `SetInt`, `Bool` → `strconv.ParseBool` и `SetBool`. Ошибку разбора возвращайте внутри `FieldError` как есть — тогда `errors.As` найдёт `*strconv.NumError`.
