package task

import (
	"slices"
	"testing"
	"testing/fstest"
)

// В программе здесь был бы embed.FS:
//
//	//go:embed greetings
//	var greetings embed.FS
//
// В тестах удобнее fstest.MapFS — файловая система в памяти.
func testFS() fstest.MapFS {
	return fstest.MapFS{
		"greetings/en.txt": {Data: []byte("Hello!\n")},
		"greetings/ru.txt": {Data: []byte("  Привет!\n")},
		"greetings/de.txt": {Data: []byte("Hallo!")},
	}
}

func TestGreeting(t *testing.T) {
	tests := []struct{ lang, want string }{
		{"ru", "Привет!"},
		{"de", "Hallo!"},
		{"en", "Hello!"},
		{"fr", "Hello!"}, // нет файла — откат на en
	}
	for _, tt := range tests {
		got, err := Greeting(testFS(), tt.lang)
		if err != nil || got != tt.want {
			t.Errorf("Greeting(%q) = %q, %v; want %q, nil", tt.lang, got, err, tt.want)
		}
	}
}

func TestLanguages(t *testing.T) {
	got, err := Languages(testFS())
	want := []string{"de", "en", "ru"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("Languages = %v, %v; want %v, nil", got, err, want)
	}
}
