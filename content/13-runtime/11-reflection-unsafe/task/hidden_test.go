package task

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInvalidTargets(t *testing.T) {
	var nilPtr *serverConfig
	n := 5
	targets := map[string]any{
		"nil":            nil,
		"nil pointer":    nilPtr,
		"pointer to int": &n,
		"int":            n,
		"map":            map[string]string{},
	}
	for name, dst := range targets {
		if err := Load(dst, map[string]string{"PORT": "1"}); !errors.Is(err, ErrInvalidTarget) {
			t.Errorf("%s: Load = %v, want ErrInvalidTarget", name, err)
		}
	}
}

func TestDurationIsNotInt(t *testing.T) {
	// Kind у time.Duration — Int64: разбор через strconv примет "5" за 5 нс.
	var c struct {
		D time.Duration `env:"D"`
	}
	if err := Load(&c, map[string]string{"D": "250ms"}); err != nil || c.D != 250*time.Millisecond {
		t.Fatalf("D = %v, err = %v; want 250ms", c.D, err)
	}
	err := Load(&c, map[string]string{"D": "5"})
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "D" {
		t.Errorf(`Load(D="5") = %v, want *FieldError: у длительности нужна единица (time.ParseDuration)`, err)
	}
}

func TestInt64AndNamedKinds(t *testing.T) {
	type level string
	var c struct {
		Big   int64 `env:"BIG"`
		Level level `env:"LEVEL"`
		Neg   int   `env:"NEG"`
		On    bool  `env:"ON"`
	}
	err := Load(&c, map[string]string{"BIG": "9000000000", "LEVEL": "debug", "NEG": "-3", "ON": "0"})
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if c.Big != 9_000_000_000 || c.Level != "debug" || c.Neg != -3 || c.On {
		t.Errorf("c = %+v", c)
	}
}

func TestParseErrors(t *testing.T) {
	type cfg struct {
		Port  int   `env:"PORT"`
		Debug bool  `env:"DEBUG"`
		Size  int64 `env:"SIZE"`
	}
	tests := []struct {
		env   map[string]string
		field string
	}{
		{map[string]string{"PORT": "eighty"}, "Port"},
		{map[string]string{"PORT": ""}, "Port"},
		{map[string]string{"DEBUG": "yes"}, "Debug"},
		{map[string]string{"SIZE": "99999999999999999999"}, "Size"},
	}
	for _, tt := range tests {
		var c cfg
		err := Load(&c, tt.env)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != tt.field {
			t.Errorf("Load(%v) = %v, want *FieldError для поля %s", tt.env, err, tt.field)
			continue
		}
		if tt.field != "Debug" {
			var ne *strconv.NumError
			if !errors.As(err, &ne) {
				t.Errorf("Load(%v): ошибка разбора числа должна оборачивать *strconv.NumError, получили %v", tt.env, err)
			}
		}
		if errors.Is(err, ErrMissing) || errors.Is(err, ErrUnsupported) {
			t.Errorf("Load(%v): ошибка разбора не должна быть ErrMissing/ErrUnsupported: %v", tt.env, err)
		}
	}
}

func TestEmptyStringIsValue(t *testing.T) {
	c := struct {
		Host string `env:"HOST,required"`
	}{Host: "default"}
	if err := Load(&c, map[string]string{"HOST": ""}); err != nil || c.Host != "" {
		t.Errorf("пустое значение заданной переменной: Host = %q, err = %v; want \"\", nil", c.Host, err)
	}
}

func TestMissingOptionalKeepsValue(t *testing.T) {
	c := struct {
		Port    int           `env:"PORT"`
		Timeout time.Duration `env:"TIMEOUT"`
	}{Port: 80, Timeout: time.Second}
	if err := Load(&c, nil); err != nil {
		t.Fatalf("Load(nil env) = %v", err)
	}
	if c.Port != 80 || c.Timeout != time.Second {
		t.Errorf("необязательные поля без переменных должны сохранить значения: %+v", c)
	}
}

func TestSkippedFields(t *testing.T) {
	c := struct {
		A string `env:"-"`
		B string `json:"b"`
		C string `env:"C"`
	}{A: "a", B: "b"}
	if err := Load(&c, map[string]string{"-": "x", "A": "x", "B": "x", "b": "x", "C": "c"}); err != nil {
		t.Fatalf("Load = %v", err)
	}
	if c.A != "a" || c.B != "b" || c.C != "c" {
		t.Errorf("c = %+v: поля без тега env и с env:\"-\" не трогаются", c)
	}
}

func TestUnsupported(t *testing.T) {
	cases := map[string]struct {
		dst   any
		field string
	}{
		"float": {&struct {
			F float64 `env:"F"`
		}{}, "F"},
		"slice": {&struct {
			S []string `env:"S"`
		}{}, "S"},
		"int32": {&struct {
			I int32 `env:"I"`
		}{}, "I"},
		"unexported": {&struct {
			secret string `env:"SECRET"`
		}{}, "secret"},
		"unknown option": {&struct {
			P int `env:"P,optional"`
		}{}, "P"},
		"empty name": {&struct {
			Q string `env:""`
		}{}, "Q"},
	}
	env := map[string]string{"F": "1.5", "S": "a,b", "I": "1", "SECRET": "x", "P": "1", "": "x"}
	for name, tc := range cases {
		err := Load(tc.dst, env)
		var fe *FieldError
		if !errors.Is(err, ErrUnsupported) || !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("%s: Load = %v, want *FieldError{Field: %s} с ErrUnsupported", name, err, tc.field)
		}
	}
	// Неподдерживаемое поле — ошибка, даже если переменной нет.
	if err := Load(&struct {
		F float64 `env:"F"`
	}{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("float без переменной: Load = %v, want ErrUnsupported", err)
	}
	if err := Load(&struct {
		secret string `env:"SECRET"`
	}{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("неэкспортируемое поле без переменной: Load = %v, want ErrUnsupported", err)
	}
}

func TestFirstErrorInFieldOrder(t *testing.T) {
	c := struct {
		A int    `env:"A"`
		B string `env:"B"`
		C int    `env:"C,required"`
		D int    `env:"D"`
	}{}
	err := Load(&c, map[string]string{"A": "1", "B": "set", "D": "oops"})
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "C" || !errors.Is(err, ErrMissing) {
		t.Fatalf("Load = %v, want первую по порядку ошибку — поле C, ErrMissing", err)
	}
	if c.A != 1 || c.B != "set" {
		t.Errorf("поля до ошибки должны быть заполнены: %+v", c)
	}
}

func TestFieldErrorMessage(t *testing.T) {
	var cfg serverConfig
	err := Load(&cfg, map[string]string{"PORT": "x"})
	if err == nil || !strings.Contains(err.Error(), "PORT") || !strings.Contains(err.Error(), "Port") {
		t.Errorf("сообщение должно называть переменную и поле: %v", err)
	}
}
