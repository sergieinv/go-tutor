package task

import "testing"

type user struct {
	Name string
	Nick string
	Tags []string
}

func TestRender(t *testing.T) {
	u := user{Name: "anna", Tags: []string{"go", "c#"}}
	tests := []struct {
		name string
		text string
		data any
		want string
	}{
		{"поле", "Привет, {{.Name}}!", u, "Привет, anna!"},
		{"upper", "{{.Name | upper}}", u, "ANNA"},
		{"join", "[{{.Tags | join \", \"}}]", u, "[go, c#]"},
		{"default пусто", "{{.Nick | default \"аноним\"}}", u, "аноним"},
		{"default есть", "{{.Nick | default \"аноним\"}}", user{Nick: "neo"}, "neo"},
		{"range", "{{range .}}<{{.}}>{{end}}", []string{"a", "b"}, "<a><b>"},
		{"map", "{{.city}}", map[string]string{"city": "Казань"}, "Казань"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.text, tt.data)
			if err != nil || got != tt.want {
				t.Errorf("Render(%q) = %q, %v; want %q, nil", tt.text, got, err, tt.want)
			}
		})
	}
}

func TestRenderParseError(t *testing.T) {
	got, err := Render("{{.Name", user{})
	if err == nil || got != "" {
		t.Errorf("Render с незакрытым действием = %q, %v; want \"\" и ошибку", got, err)
	}
}
