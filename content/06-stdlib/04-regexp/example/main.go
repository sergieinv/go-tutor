// Пакет `regexp` реализует синтаксис RE2: время поиска
// линейно от длины входа, катастрофического бэктрекинга
// не бывает. Цена — нет обратных ссылок и lookaround.

package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Выражения компилируют один раз — в переменную пакета.
// `MustCompile` паникует на неверном шаблоне: ошибка
// в литерале — баг программы, и он всплывёт при старте.
// Сырые строки в обратных кавычках избавляют от `\\`.
var (
	dateRe = regexp.MustCompile(`(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})`)
	wordRe = regexp.MustCompile(`\p{L}+`)
	tagRe  = regexp.MustCompile(`#(\w+)`)
)

func main() {
	text := "Релиз 2024-03-15, хотфикс 2024-04-02. #go #release"

	// `MatchString` ищет совпадение где угодно в строке;
	// для проверки всей строки нужны якоря `^…$`.
	fmt.Println(dateRe.MatchString(text), dateRe.FindString(text))

	// `FindAllString(s, n)`: `n` — сколько максимум найти,
	// `-1` — все. Без совпадений результат `nil`.
	fmt.Println(dateRe.FindAllString(text, -1))
	fmt.Println(wordRe.FindAllString("Привет, мир!", 1))

	// `Submatch`-варианты возвращают ещё и группы: `m[0]` —
	// всё совпадение, `m[1]`… — группы по порядку скобок.
	for _, m := range dateRe.FindAllStringSubmatch(text, -1) {
		fmt.Println(m[1], m[2], m[3])
	}

	// Именованные группы `(?P<name>…)` (или `(?<name>…)`
	// с Go 1.22): индекс группы по имени — `SubexpIndex`.
	m := dateRe.FindStringSubmatch(text)
	fmt.Println("месяц:", m[dateRe.SubexpIndex("month")])

	// В строке замены `$1` и `${name}` — ссылки на группы.
	fmt.Println(dateRe.ReplaceAllString(text, "${day}.${month}.${year}"))

	// `ReplaceAllStringFunc` вычисляет замену функцией,
	// но получает только всё совпадение, без групп.
	fmt.Println(tagRe.ReplaceAllStringFunc(text, strings.ToUpper))

	// `QuoteMeta` экранирует спецсимволы — для поиска
	// пользовательского текста как литерала.
	lit := regexp.MustCompile(regexp.QuoteMeta("1+1=2?"))
	fmt.Println(lit.String(), lit.MatchString("а 1+1=2? б"))

	// Lookbehind и обратные ссылки RE2 не поддерживает:
	// `Compile` вернёт ошибку, а не выражение.
	_, err := regexp.Compile(`(\w)\1`)
	fmt.Println(err)
}
