---
title: XML
summary: encoding/xml — теги attr, chardata, innerxml, comment, вложенность a>b и XMLName для имени корня.
difficulty: 2
minutes: 15
prerequisites:
  - 06-stdlib/05-json
docs:
  - title: "Пакет encoding/xml"
    url: https://pkg.go.dev/encoding/xml
---

XML в Go устроен так же, как JSON: `xml.Marshal` и `xml.Unmarshal`, правила — в тегах `xml:"..."`, видны только экспортируемые поля. Новое — то, чего нет в JSON: атрибуты (`,attr`), текст внутри элемента (`,chardata`), сырое содержимое (`,innerxml`), комментарии (`,comment`) и путь через вложенные элементы `a>b`.

Имя корневого элемента берётся из поля `XMLName xml.Name` (с тегом), а если его нет — из имени типа. При разборе тег на `XMLName` работает как проверка: другой корень — ошибка.

## Как это в C#

- `XmlSerializer` с атрибутами `[XmlRoot("book")]`, `[XmlAttribute("id")]`, `[XmlText]`, `[XmlElement]` → поле `XMLName` и теги `xml:"id,attr"`, `xml:",chardata"`, `xml:"title"`.
- `[XmlArray("authors"), XmlArrayItem("author")]` → один тег `xml:"authors>author"` на срезе.
- Аналога `XDocument`/LINQ to XML (дерево в памяти) в стандартной библиотеке нет. Произвольные документы читают `xml.Decoder` с `Token()` — потоковый разбор по токенам, как `XmlReader`.
- `XmlSerializer` требует публичный конструктор без параметров и сериализует публичные свойства и поля; в Go — экспортируемые поля, конструкторы не нужны.

## Ловушки

### `chardata` собирает только свой текст

```go
type title struct {
	Text string `xml:",chardata"`
}
// <title>Шаблоны <i>Go</i></title> → Text == "Шаблоны "
```

Текст вложенных элементов в `chardata` не попадает. Нужна вся разметка внутри — `,innerxml`; нужен весь текст без разметки — разбирайте токены `xml.Decoder`.

### Нет заголовка `<?xml …?>`

```go
out, _ := xml.Marshal(v)
os.Stdout.Write(out) // без <?xml version="1.0" encoding="UTF-8"?>
```

`Marshal` выдаёт только элементы. Если получатель требует объявление, допишите `xml.Header` сами.

### `omitempty` не убирает обёртку `a>b`

```go
type entry struct {
	Tags []string `xml:"tags>tag,omitempty"`
}
// entry{} → <entry><tags></tags></entry>
```

Промежуточный элемент `tags` пишется до того, как проверяется `omitempty`, поэтому пустая обёртка остаётся. Чтобы убрать её, вынесите обёртку в отдельный тип и храните указатель: ``Tags *tagList `xml:"tags"` ``, — `nil`-указатель не выводится вовсе.

## Задание

Разберите ленту записей и сериализуйте одну запись.

```go
type Feed struct {
	Version string
	Title   string
	Entries []Entry
}

type Entry struct {
	ID    int
	Draft bool
	Title string
	Tags  []string
}
```

Формат документа:

```xml
<feed version="2">
  <title>Блог</title>
  <entry id="1" draft="true">
    <title>Первый</title>
    <tags><tag>go</tag><tag>xml</tag></tags>
  </entry>
</feed>
```

`func ParseFeed(data []byte) (Feed, error)`:

- `Version` — атрибут `version` корня, `Title` — элемент `title`, `Entries` — все элементы `entry` в порядке документа;
- у записи: `ID` — атрибут `id`, `Draft` — атрибут `draft`, `Title` — элемент `title`, `Tags` — элементы `tag` внутри `tags`;
- отсутствующие атрибуты и элементы дают нулевые значения, неизвестные — игнорируются;
- корневой элемент не `feed`, некорректный XML или нечисловой `id` — ошибка.

Можно добавить в `Feed` и `Entry` поле `XMLName` — тесты сравнивают только перечисленные поля.

`func EncodeEntry(e Entry) (string, error)` возвращает XML записи без отступов и переводов строк, в порядке: атрибут `id`, атрибут `draft`, элемент `title`, элемент `tags`. Атрибут `draft` выводится только при `true`; элементы `title` и `tags` — всегда, при пустом или `nil` `Tags` — как `<tags></tags>`. Спецсимволы экранирует `encoding/xml`. Пример: `<entry id="1" draft="true"><title>A &amp; B</title><tags><tag>go</tag></tags></entry>`.
