// Переменные окружения — строки «ключ=значение», которые
// процесс получает от родителя. В Go с ними работают функции
// пакета `os`; типизированной обёртки нет — разбор чисел и
// длительностей пишется явно.

package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Типичный загрузчик конфигурации: значение из окружения
// или значение по умолчанию.
func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func main() {
	os.Setenv("APP_PORT", "9000")
	os.Setenv("APP_DEBUG", "") // задана, но пустая
	os.Unsetenv("APP_TOKEN")   // точно не задана

	// `Getenv` возвращает "" и для пустой, и для
	// незаданной переменной — различить их нельзя.
	fmt.Printf("Getenv: %q %q\n", os.Getenv("APP_DEBUG"), os.Getenv("APP_TOKEN"))

	// `LookupEnv` возвращает ещё и признак «задана».
	_, debugSet := os.LookupEnv("APP_DEBUG")
	_, tokenSet := os.LookupEnv("APP_TOKEN")
	fmt.Println("LookupEnv:", debugSet, tokenSet)

	// Всё окружение — срез строк "KEY=value". Порядок не
	// определён, поэтому фильтруем и сортируем.
	var app []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "APP_") {
			app = append(app, kv)
		}
	}
	slices.Sort(app)
	fmt.Println("Environ:", app)

	// `ExpandEnv` подставляет `$VAR` и `${VAR}`; незаданные
	// превращаются в пустую строку.
	fmt.Println(os.ExpandEnv("addr=${APP_HOST}:$APP_PORT"))

	// `os.Expand` берёт значения из своей функции — например,
	// с подстановкой значений по умолчанию.
	defaults := map[string]string{"APP_HOST": "localhost"}
	fmt.Println(os.Expand("addr=${APP_HOST}:$APP_PORT", func(k string) string {
		return getenv(k, defaults[k])
	}))

	// Значения — строки. Числа, флаги и длительности
	// разбирают явно и проверяют ошибку.
	port, err := strconv.Atoi(getenv("APP_PORT", "8080"))
	fmt.Println("port:", port, err)
	timeout, err := time.ParseDuration(getenv("APP_TIMEOUT", "5s"))
	fmt.Println("timeout:", timeout, err)
	_, err = strconv.ParseBool(getenv("APP_DEBUG", "false"))
	fmt.Println("debug:", err) // пустая строка — не bool

	// Изменения видны этому процессу и его будущим дочерним
	// процессам, но не родителю (оболочке, из которой запустили).
	os.Setenv("APP_PORT", "9100")
	fmt.Println("после Setenv:", os.Getenv("APP_PORT"))
}
