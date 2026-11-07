package text

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

// Табличный тест: случаи — срез анонимных структур,
// проверка — один цикл. Новый случай — одна строка.
// Поле `name` объясняет, что проверяет случай. Один
// случай запускают так: `go test -run 'TestSlug/пробелы_по_краям'`
// — шаблон режется по `/`, каждая часть — регулярное
// выражение для своего уровня имени.
func TestSlug(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"простой", "Hello World", "hello-world"},
		{"пробелы по краям", "  Go  ", "go"},
		{"знаки подряд", "Go: тесты, табл.", "go-тесты-табл"},
		{"цифры", "Go 1.25", "go-1-25"},
		{"пусто", "", ""},
	}
	for _, tt := range tests {
		// `t.Run` запускает подтест со своим `t`: его провал
		// не мешает остальным случаям, а `Fatalf` внутри
		// остановит только этот случай. Пробелы в имени
		// заменяются на `_`: `TestSlug/пробелы_по_краям`.
		t.Run(tt.name, func(t *testing.T) {
			if got := Slug(tt.title); got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

// Срезы `==` не сравнить — есть `slices.Equal`. Он считает
// `nil` и пустой срез равными, а `reflect.DeepEqual` — нет:
// это частая причина «необъяснимо» красных тестов.
func TestWords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"два слова", "Go test", []string{"go", "test"}},
		{"пусто", "", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Words(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Words(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Для map — `maps.Equal`. `t.Run` возвращает `true`, если
// подтест прошёл: так можно не запускать остальное, когда
// сломан базовый случай.
func TestFreq(t *testing.T) {
	ok := t.Run("базовый", func(t *testing.T) {
		got := Freq("Go go GO")
		want := map[string]int{"go": 3}
		if !maps.Equal(got, want) {
			t.Fatalf("Freq(%q) = %v, want %v", "Go go GO", got, want)
		}
	})
	if !ok {
		t.FailNow()
	}
	// `reflect.DeepEqual` сравнивает что угодно рекурсивно:
	// map, вложенные срезы, структуры. Но он не типизирован
	// и различает `nil` и пустые срезы и map.
	t.Run("разные слова", func(t *testing.T) {
		got := Freq("a b a")
		if want := map[string]int{"a": 2, "b": 1}; !reflect.DeepEqual(got, want) {
			t.Errorf("Freq(%q) = %v, want %v", "a b a", got, want)
		}
	})
}
