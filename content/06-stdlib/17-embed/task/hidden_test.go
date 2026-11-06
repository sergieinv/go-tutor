package task

import (
	"errors"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"
)

func TestGreetingBadLang(t *testing.T) {
	for _, lang := range []string{"", "../secret", "en/../ru", "EN", "ru.txt", "zh_CN", "/en", "."} {
		got, err := Greeting(testFS(), lang)
		if !errors.Is(err, ErrBadLang) || got != "" {
			t.Errorf("Greeting(%q) = %q, %v; want \"\", ErrBadLang", lang, got, err)
		}
	}
}

func TestGreetingDashLang(t *testing.T) {
	fsys := testFS()
	fsys["greetings/pt-br.txt"] = &fstest.MapFile{Data: []byte("Olá!\n")}
	got, err := Greeting(fsys, "pt-br")
	if err != nil || got != "Olá!" {
		t.Errorf("Greeting(\"pt-br\") = %q, %v; want \"Olá!\", nil", got, err)
	}
}

func TestGreetingNoFallback(t *testing.T) {
	fsys := fstest.MapFS{"greetings/ru.txt": {Data: []byte("Привет")}}
	got, err := Greeting(fsys, "fr")
	if !errors.Is(err, fs.ErrNotExist) || got != "" {
		t.Errorf("Greeting без en.txt = %q, %v; want \"\" и fs.ErrNotExist", got, err)
	}
}

// deniedFS отказывает в доступе к одному файлу.
type deniedFS struct {
	m    fstest.MapFS
	path string
}

func (d deniedFS) Open(name string) (fs.File, error) {
	if name == d.path {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return d.m.Open(name)
}

func TestGreetingOtherError(t *testing.T) {
	fsys := deniedFS{m: testFS(), path: "greetings/de.txt"}
	got, err := Greeting(fsys, "de")
	if !errors.Is(err, fs.ErrPermission) || got != "" {
		t.Errorf("Greeting = %q, %v; want \"\" и ошибку доступа без отката на en", got, err)
	}
}

func TestLanguagesFilter(t *testing.T) {
	fsys := fstest.MapFS{
		"greetings/ru.txt":     {Data: []byte("Привет")},
		"greetings/es.txt":     {Data: []byte("Hola")},
		"greetings/README.md":  {Data: []byte("docs")},
		"greetings/.txt":       {Data: []byte("пустой код")},
		"greetings/EN.txt":     {Data: []byte("Hello")},
		"greetings/old/fr.txt": {Data: []byte("Salut")},
		"other/it.txt":         {Data: []byte("Ciao")},
	}
	got, err := Languages(fsys)
	want := []string{"es", "ru"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("Languages = %v, %v; want %v, nil", got, err, want)
	}
}

func TestLanguagesEmpty(t *testing.T) {
	got, err := Languages(fstest.MapFS{"readme.txt": {}})
	if err != nil || len(got) != 0 {
		t.Errorf("Languages без каталога greetings = %v, %v; want пустой результат и nil", got, err)
	}
}
