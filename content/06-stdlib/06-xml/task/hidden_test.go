package task

import (
	"slices"
	"testing"
)

func TestParseFeedErrors(t *testing.T) {
	bad := []string{
		`<rss version="2"><title>x</title></rss>`,
		`<feed><entry id="x"/></feed>`,
		`<feed><title>x</feed>`,
		``,
	}
	for _, in := range bad {
		if f, err := ParseFeed([]byte(in)); err == nil {
			t.Errorf("ParseFeed(%q) = %+v, nil; want ошибку", in, f)
		}
	}
}

func TestParseFeedMissingAndExtra(t *testing.T) {
	in := `<feed lang="ru"><unknown>?</unknown><entry><tags></tags></entry><entry id="7" draft="false" x="y"><title>T</title><tags><tag>a</tag></tags><body>b</body></entry></feed>`
	f, err := ParseFeed([]byte(in))
	if err != nil {
		t.Fatalf("ParseFeed вернул ошибку %v", err)
	}
	if f.Version != "" || f.Title != "" || len(f.Entries) != 2 {
		t.Fatalf("ParseFeed = %+v", f)
	}
	if e := f.Entries[0]; e.ID != 0 || e.Draft || e.Title != "" || len(e.Tags) != 0 {
		t.Errorf("пустая запись = %+v, want нулевые поля", e)
	}
	if e := f.Entries[1]; e.ID != 7 || e.Draft || e.Title != "T" || !slices.Equal(e.Tags, []string{"a"}) {
		t.Errorf("вторая запись = %+v", e)
	}
}

func TestParseFeedEmpty(t *testing.T) {
	f, err := ParseFeed([]byte(`<feed version="1"/>`))
	if err != nil || f.Version != "1" || len(f.Entries) != 0 {
		t.Errorf("ParseFeed(<feed/>) = %+v, %v", f, err)
	}
}

func TestEncodeEntryOmit(t *testing.T) {
	tests := []struct {
		in   Entry
		want string
	}{
		{Entry{ID: 3, Title: "x"}, `<entry id="3"><title>x</title><tags></tags></entry>`},
		{Entry{}, `<entry id="0"><title></title><tags></tags></entry>`},
		{Entry{ID: 4, Title: `<"я">`, Tags: []string{}}, `<entry id="4"><title>&lt;&#34;я&#34;&gt;</title><tags></tags></entry>`},
		{Entry{ID: 5, Tags: []string{"a", "b"}}, `<entry id="5"><title></title><tags><tag>a</tag><tag>b</tag></tags></entry>`},
	}
	for _, tt := range tests {
		got, err := EncodeEntry(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("EncodeEntry(%+v) = %s, %v; want %s", tt.in, got, err, tt.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := Entry{ID: 9, Draft: true, Title: "R", Tags: []string{"x", "y"}}
	s, err := EncodeEntry(in)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFeed([]byte("<feed>" + s + "</feed>"))
	if err != nil || len(f.Entries) != 1 {
		t.Fatalf("ParseFeed(<feed>EncodeEntry</feed>) = %+v, %v", f, err)
	}
	e := f.Entries[0]
	if e.ID != in.ID || e.Draft != in.Draft || e.Title != in.Title || !slices.Equal(e.Tags, in.Tags) {
		t.Errorf("после круга = %+v, want %+v", e, in)
	}
}
