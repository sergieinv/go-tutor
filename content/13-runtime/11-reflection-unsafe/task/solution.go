package task

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidTarget — dst не является ненулевым указателем на структуру.
	ErrInvalidTarget = errors.New("target must be a non-nil pointer to a struct")
	// ErrMissing — обязательная переменная не задана.
	ErrMissing = errors.New("required variable is not set")
	// ErrUnsupported — поле нельзя заполнить: тип не поддерживается,
	// поле не экспортировано или тег некорректен.
	ErrUnsupported = errors.New("unsupported field")
)

// FieldError описывает ошибку конкретного поля.
type FieldError struct {
	Field string // имя поля структуры
	Key   string // имя переменной из тега
	Err   error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("env %s (field %s): %v", e.Key, e.Field, e.Err)
}

func (e *FieldError) Unwrap() error { return e.Err }

var durationType = reflect.TypeFor[time.Duration]()

// Load заполняет поля структуры *dst из env по тегам `env:"NAME[,required]"`.
func Load(dst any, env map[string]string) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return ErrInvalidTarget
	}
	sv := rv.Elem()
	st := sv.Type()
	for i := range st.NumField() {
		sf := st.Field(i)
		tag, ok := sf.Tag.Lookup("env")
		if !ok || tag == "-" {
			continue
		}
		key, opts, _ := strings.Cut(tag, ",")
		required := false
		if opts != "" {
			if opts != "required" {
				return &FieldError{Field: sf.Name, Key: key, Err: fmt.Errorf("%w: unknown tag option %q", ErrUnsupported, opts)}
			}
			required = true
		}
		if key == "" {
			return &FieldError{Field: sf.Name, Key: key, Err: fmt.Errorf("%w: empty variable name", ErrUnsupported)}
		}
		// Неэкспортируемое поле reflect менять не позволит (CanSet = false).
		fv := sv.Field(i)
		if !fv.CanSet() {
			return &FieldError{Field: sf.Name, Key: key, Err: fmt.Errorf("%w: field is not exported", ErrUnsupported)}
		}
		if !supported(fv.Type()) {
			return &FieldError{Field: sf.Name, Key: key, Err: fmt.Errorf("%w: type %s", ErrUnsupported, fv.Type())}
		}
		raw, ok := env[key]
		if !ok {
			if required {
				return &FieldError{Field: sf.Name, Key: key, Err: ErrMissing}
			}
			continue
		}
		if err := set(fv, raw); err != nil {
			return &FieldError{Field: sf.Name, Key: key, Err: err}
		}
	}
	return nil
}

// supported сообщает, умеет ли set разбирать значения такого типа.
func supported(t reflect.Type) bool {
	if t == durationType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Int, reflect.Int64, reflect.Bool:
		return true
	}
	return false
}

// set разбирает строку в значение поля. Duration проверяется до Kind:
// его Kind — Int64, и strconv приняло бы "5s" за ошибку, а "5" — за 5 нс.
func set(fv reflect.Value, raw string) error {
	if fv.Type() == durationType {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		fv.SetInt(int64(d))
		return nil
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	}
	return nil
}
