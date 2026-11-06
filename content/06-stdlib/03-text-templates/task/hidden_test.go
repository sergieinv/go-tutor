package task

import "testing"

func TestRenderMissingKey(t *testing.T) {
	got, err := Render("{{.city}}", map[string]string{"town": "x"})
	if err == nil || got != "" {
		t.Errorf("Render с отсутствующим ключом = %q, %v; want \"\" и ошибку", got, err)
	}
}

func TestRenderNoPartialOutput(t *testing.T) {
	got, err := Render("начало {{.a}} {{.b}} конец", map[string]int{"a": 1})
	if err == nil {
		t.Fatalf("Render = %q, nil; want ошибку", got)
	}
	if got != "" {
		t.Errorf("при ошибке Render вернул частичный вывод %q, want \"\"", got)
	}
}

func TestRenderExecError(t *testing.T) {
	got, err := Render("{{.Phone}}", user{Name: "x"})
	if err == nil || got != "" {
		t.Errorf("Render с неизвестным полем = %q, %v; want \"\" и ошибку", got, err)
	}
}

func TestRenderUnknownFunc(t *testing.T) {
	if _, err := Render("{{lower .Name}}", user{}); err == nil {
		t.Error("Render с неизвестной функцией: want ошибку")
	}
}

func TestRenderFuncsCombined(t *testing.T) {
	tests := []struct {
		text string
		data any
		want string
	}{
		{"[{{.Tags | join \"|\"}}]", user{}, "[]"},
		{"{{join \"-\" .Tags}}", user{Tags: []string{"a", "b", "c"}}, "a-b-c"},
		{"{{.Nick | default .Name | upper}}", user{Name: "ivan"}, "IVAN"},
		{"{{default \"x\" \"y\"}}", nil, "y"},
		{"{{range $i, $t := .Tags}}{{if $i}}, {{end}}{{upper $t}}{{end}}", user{Tags: []string{"a", "b"}}, "A, B"},
		{"{{with .Nick}}ник {{.}}{{else}}без ника{{end}}", user{}, "без ника"},
		{"просто текст", nil, "просто текст"},
		{"", nil, ""},
	}
	for _, tt := range tests {
		got, err := Render(tt.text, tt.data)
		if err != nil || got != tt.want {
			t.Errorf("Render(%q) = %q, %v; want %q, nil", tt.text, got, err, tt.want)
		}
	}
}
