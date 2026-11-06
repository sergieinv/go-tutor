package task

import "encoding/xml"

// Feed — лента записей.
type Feed struct {
	// Тег на XMLName проверяет имя корня при разборе.
	XMLName xml.Name `xml:"feed"`
	Version string   `xml:"version,attr"`
	Title   string   `xml:"title"`
	Entries []Entry  `xml:"entry"`
}

// Entry — запись ленты.
type Entry struct {
	XMLName xml.Name `xml:"entry"`
	ID      int      `xml:"id,attr"`
	Draft   bool     `xml:"draft,attr,omitempty"`
	Title   string   `xml:"title"`
	// Обёртка tags выводится и для пустого среза.
	Tags []string `xml:"tags>tag"`
}

// ParseFeed разбирает XML-документ с корнем <feed>.
func ParseFeed(data []byte) (Feed, error) {
	var f Feed
	if err := xml.Unmarshal(data, &f); err != nil {
		return Feed{}, err
	}
	return f, nil
}

// EncodeEntry возвращает XML одной записи без отступов.
func EncodeEntry(e Entry) (string, error) {
	b, err := xml.Marshal(e)
	return string(b), err
}
