// У этого примера свой `go.mod`: он зависит от
// `golang.org/x/text` версии v0.3.7. В ней функция
// `language.ParseAcceptLanguage` уязвима: специально
// составленный заголовок `Accept-Language` заставляет её
// работать очень долго (GO-2022-1059, отказ в
// обслуживании). Нажмите «govulncheck» — он найдёт вызов.

package main

import (
	"fmt"

	"golang.org/x/text/language"
)

// Языки, на которых сервис умеет отвечать. Первый —
// язык по умолчанию, если клиенту ничего не подошло.
var matcher = language.NewMatcher([]language.Tag{
	language.English,
	language.Russian,
	language.German,
})

// Заголовок приходит от клиента — это граница доверия.
// Свой лимит длины — хорошая привычка, но govulncheck
// его не видит: он ищет вызовы уязвимых функций и
// сообщит о них, пока версия модуля уязвима.
func pickLanguage(header string) string {
	if len(header) > 256 {
		header = ""
	}
	// УЯЗВИМО: в x/text v0.3.7 этот вызов — отказ в
	// обслуживании; govulncheck покажет эту строку как
	// трассу. Исправление — обновить модуль до v0.3.8.
	tags, _, err := language.ParseAcceptLanguage(header)
	if err != nil {
		return "en (заголовок не разобран)"
	}
	tag, _, _ := matcher.Match(tags...)
	base, _ := tag.Base()
	return base.String()
}

// Пакет `language` здесь используется и безопасно:
// `Make` и `Base` в уязвимость не входят. Символьный
// анализ различает функции, а не только модули.
func main() {
	for _, h := range []string{
		"ru-RU,ru;q=0.9,en;q=0.5",
		"de-CH, fr;q=0.8",
		"ja",
		"",
	} {
		fmt.Printf("%-24q → %s\n", h, pickLanguage(h))
	}
	base, _ := language.Make("pt-BR").Base()
	fmt.Println("Make:", base)
}
