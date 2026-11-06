package task

import (
	"slices"
	"testing"
)

func TestParseConfig(t *testing.T) {
	c, err := ParseConfig([]byte(`{"name":"api","port":9000,"tags":["a","b"],"debug":true}`))
	if err != nil {
		t.Fatalf("ParseConfig вернул ошибку %v", err)
	}
	if c.Name != "api" || c.Port != 9000 || !slices.Equal(c.Tags, []string{"a", "b"}) || !c.Debug {
		t.Errorf("ParseConfig = %+v", c)
	}
}

func TestParseConfigDefaultPort(t *testing.T) {
	c, err := ParseConfig([]byte(`{"name":"api"}`))
	if err != nil || c.Port != 8080 {
		t.Errorf("ParseConfig без port = %+v, %v; want Port 8080", c, err)
	}
}

func TestParseConfigUnknownKey(t *testing.T) {
	if _, err := ParseConfig([]byte(`{"name":"api","host":"x"}`)); err == nil {
		t.Error("ParseConfig с неизвестным ключом: want ошибку")
	}
}

func TestEncodeConfig(t *testing.T) {
	tests := []struct {
		in   Config
		want string
	}{
		{Config{Name: "api", Port: 8080}, `{"name":"api","port":8080}`},
		{Config{Name: "w", Port: 1, Tags: []string{"x"}, Debug: true}, `{"name":"w","port":1,"tags":["x"],"debug":true}`},
	}
	for _, tt := range tests {
		got, err := EncodeConfig(tt.in)
		if err != nil || string(got) != tt.want {
			t.Errorf("EncodeConfig(%+v) = %s, %v; want %s", tt.in, got, err, tt.want)
		}
	}
}
