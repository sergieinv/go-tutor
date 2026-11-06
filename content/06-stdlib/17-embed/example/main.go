// Директива `//go:embed` вшивает файлы в бинарник на этапе
// сборки. Программа носит данные с собой: шаблоны, SQL-миграции,
// статику для веб-сервера — и не ищет их на диске.

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

// Файл в переменную-строку. Директива стоит прямо над
// `var`, путь — относительно каталога пакета. Пакет
// `embed` нужно импортировать, хотя бы через `_`.
//
//go:embed greeting.txt
var greeting string

// Тот же приём со срезом байт — для бинарных данных
// или для разбора, например, JSON.
//
//go:embed config.json
var configJSON []byte

// `embed.FS` — набор файлов по шаблонам. Он реализует
// `fs.FS`, поэтому с ним работают `fs.ReadFile`,
// `fs.Glob`, `fs.WalkDir`, `template.ParseFS`, `http.FS`.
//
//go:embed *.txt
var texts embed.FS

func main() {
	fmt.Print("string: ", greeting)

	var cfg struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		panic(err)
	}
	fmt.Printf("[]byte: %d байт, %+v\n", len(configJSON), cfg)

	// Пути внутри `embed.FS` — со слешами, относительно
	// каталога пакета. Список отсортирован по имени.
	entries, _ := texts.ReadDir(".")
	for _, e := range entries {
		data, _ := texts.ReadFile(e.Name())
		fmt.Printf("FS: %s → %q\n", e.Name(), strings.TrimSpace(string(data)))
	}

	// config.json не попал под шаблон `*.txt` — в этой FS его нет.
	_, err := fs.ReadFile(texts, "config.json")
	fmt.Println("config.json в texts:", err)

	// Встроенные данные только для чтения: у `embed.FS` нет
	// методов записи, а переменная-строка — копия на момент сборки.
	fmt.Println("файлов по *.txt:", must(fs.Glob(texts, "*.txt")))
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
