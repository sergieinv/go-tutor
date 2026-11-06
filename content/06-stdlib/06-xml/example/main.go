// `encoding/xml` устроен как `encoding/json`: Marshal,
// Unmarshal и struct tags. Отличия — в тегах: у XML есть
// атрибуты, текст элемента и вложенность.

package main

import (
	"encoding/xml"
	"fmt"
)

// `XMLName` задаёт имя корневого элемента.
// `,attr` — атрибут, `,chardata` — текст внутри элемента,
// `a>b` — вложенные элементы без промежуточной структуры,
// `,omitempty` — пропустить пустое, `,comment` — комментарий.
type book struct {
	XMLName xml.Name `xml:"book"`
	ID      string   `xml:"id,attr"`
	Lang    string   `xml:"lang,attr,omitempty"`
	Title   title    `xml:"title"`
	Authors []string `xml:"authors>author"`
	Note    string   `xml:",comment"`
}

type title struct {
	Sub  bool   `xml:"sub,attr,omitempty"`
	Text string `xml:",chardata"`
}

func main() {
	b := book{
		ID:      "go-1",
		Title:   title{Text: "Go & C#"},
		Authors: []string{"Анна", "Иван"},
		Note:    " черновик ",
	}
	// `MarshalIndent` — с отступами; спецсимволы вроде `&`
	// экранируются автоматически. Заголовок `<?xml …?>`
	// не добавляется — для него есть константа `xml.Header`.
	out, err := xml.MarshalIndent(b, "", "  ")
	fmt.Println(string(out), err)

	// Разбор: лишние элементы и атрибуты игнорируются,
	// отсутствующие оставляют поля нулевыми.
	src := `<book id="b2" lang="ru" extra="x">
	  <title sub="true">Шаблоны <i>Go</i></title>
	  <authors><author>Вера</author></authors>
	</book>`
	var got book
	err = xml.Unmarshal([]byte(src), &got)
	fmt.Printf("%s %s %v %q %v %v\n", got.ID, got.Lang, got.Title.Sub,
		got.Title.Text, got.Authors, err)

	// `,innerxml` сохраняет содержимое элемента как есть,
	// вместе с вложенной разметкой.
	var raw struct {
		Title struct {
			Inner string `xml:",innerxml"`
		} `xml:"title"`
	}
	xml.Unmarshal([]byte(src), &raw)
	fmt.Println(raw.Title.Inner)

	// Неверный корень — ошибка: `XMLName` проверяется.
	err = xml.Unmarshal([]byte(`<film id="1"/>`), &got)
	fmt.Println(err)
}
