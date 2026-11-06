package task

import (
	"slices"
	"testing"
)

const sample = `<feed version="2">
  <title>Блог</title>
  <entry id="1" draft="true">
    <title>Первый</title>
    <tags><tag>go</tag><tag>xml</tag></tags>
  </entry>
  <entry id="2">
    <title>Второй</title>
  </entry>
</feed>`

func TestParseFeed(t *testing.T) {
	f, err := ParseFeed([]byte(sample))
	if err != nil {
		t.Fatalf("ParseFeed вернул ошибку %v", err)
	}
	if f.Version != "2" || f.Title != "Блог" || len(f.Entries) != 2 {
		t.Fatalf("ParseFeed = %+v", f)
	}
	e := f.Entries[0]
	if e.ID != 1 || !e.Draft || e.Title != "Первый" || !slices.Equal(e.Tags, []string{"go", "xml"}) {
		t.Errorf("первая запись = %+v", e)
	}
	e = f.Entries[1]
	if e.ID != 2 || e.Draft || e.Title != "Второй" || len(e.Tags) != 0 {
		t.Errorf("вторая запись = %+v", e)
	}
}

func TestEncodeEntry(t *testing.T) {
	got, err := EncodeEntry(Entry{ID: 1, Draft: true, Title: "A & B", Tags: []string{"go"}})
	want := `<entry id="1" draft="true"><title>A &amp; B</title><tags><tag>go</tag></tags></entry>`
	if err != nil || got != want {
		t.Errorf("EncodeEntry = %s, %v; want %s", got, err, want)
	}
}
