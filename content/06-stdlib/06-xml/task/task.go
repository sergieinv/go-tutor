package task

// Feed — лента записей. Добавьте struct tags (и при желании XMLName).
type Feed struct {
	Version string
	Title   string
	Entries []Entry
}

// Entry — запись ленты.
type Entry struct {
	ID    int
	Draft bool
	Title string
	Tags  []string
}

// ParseFeed разбирает XML-документ с корнем <feed>.
func ParseFeed(data []byte) (Feed, error) {
	// TODO: реализуйте функцию
	return Feed{}, nil
}

// EncodeEntry возвращает XML одной записи без отступов.
func EncodeEntry(e Entry) (string, error) {
	// TODO: реализуйте функцию
	return "", nil
}
