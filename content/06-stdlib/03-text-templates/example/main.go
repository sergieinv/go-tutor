// `text/template` — шаблонизатор стандартной библиотеки.
// Текст копируется как есть, а в `{{ }}` стоят действия:
// подстановка полей, условия, циклы, вызовы функций.

package main

import (
	"fmt"
	"html/template"
	"os"
	"strings"
	ttemplate "text/template"
)

type item struct {
	Name  string
	Price float64
	Tags  []string
}

type order struct {
	ID       int
	Customer string
	Items    []item
	Note     string
}

// Шаблон парсится один раз, а исполняется много раз.
// `.` — текущие данные; `{{- ` и ` -}}` съедают пробелы
// и переводы строк с соответствующей стороны.
//
// `range` перебирает срез, внутри него `.` — элемент;
// `$i, $it :=` дают индекс и элемент. Внешние данные
// доступны через `$` — корень.
//
// Конвейер `|` передаёт результат последним аргументом
// следующей функции: `.Name | upper` — это `upper .Name`.
const text = `Заказ №{{.ID}} для {{.Customer | upper}}
{{range $i, $it := .Items -}}
{{inc $i}}. {{$it.Name}} — {{printf "%.2f" $it.Price}}
{{- if $it.Tags}} [{{join $it.Tags ", "}}]{{end}}
{{else -}}
пусто
{{end -}}
{{with .Note}}Примечание: {{.}}
{{end -}}
{{template "footer" $}}`

// Именованный вложенный шаблон: `define` объявляет,
// `template` вызывает с переданными данными.
const footer = `{{define "footer"}}Позиций: {{len .Items}}{{end}}`

func main() {
	// Свои функции подключаются через `FuncMap` до `Parse`.
	funcs := ttemplate.FuncMap{
		"upper": strings.ToUpper,
		"join":  strings.Join,
		"inc":   func(i int) int { return i + 1 },
	}
	t := ttemplate.Must(ttemplate.New("order").Funcs(funcs).Parse(text))
	ttemplate.Must(t.Parse(footer))

	o := order{ID: 42, Customer: "Анна", Items: []item{
		{"Кофе", 3.5, []string{"горячее"}},
		{"Круассан", 2.25, nil},
	}}
	if err := t.Execute(os.Stdout, o); err != nil {
		fmt.Println("ошибка:", err)
	}
	fmt.Println()

	// `with` пропускает блок для пустого значения, а `range`
	// по пустому срезу выполняет ветку `else`.
	_ = t.Execute(os.Stdout, order{ID: 7, Customer: "Иван", Note: "срочно"})
	fmt.Println()

	// Ошибки исполнения — не паника: например, обращение
	// к несуществующему полю. Проверяйте результат `Execute`.
	bad := ttemplate.Must(ttemplate.New("bad").Parse("{{.Phone}}"))
	fmt.Println(bad.Execute(os.Stdout, o))

	// `html/template` с тем же API экранирует данные
	// по контексту — защита от XSS, как в Razor.
	h := template.Must(template.New("h").Parse(`<p title="{{.}}">{{.}}</p>`))
	_ = h.Execute(os.Stdout, `<script>"x"</script>`)
	fmt.Println()
}
