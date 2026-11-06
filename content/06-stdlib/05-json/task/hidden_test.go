package task

import (
	"reflect"
	"testing"
)

func TestParseConfigErrors(t *testing.T) {
	bad := []string{
		``,
		`null`,
		`{}`,
		`{"name":""}`,
		`{"name":"a","port":"80"}`,
		`{"name":"a","port":-1}`,
		`{"name":"a","port":65536}`,
		`{"name":"a","tags":"x"}`,
		`{"name":"a"} {"name":"b"}`,
		`{"name":"a"}]`,
		`{"name":"a"}x`,
		`{"name":"a","Secret":1}`,
		`[{"name":"a"}]`,
	}
	for _, in := range bad {
		c, err := ParseConfig([]byte(in))
		if err == nil {
			t.Errorf("ParseConfig(%q) = %+v, nil; want ошибку", in, c)
			continue
		}
		if !reflect.DeepEqual(c, Config{}) {
			t.Errorf("ParseConfig(%q) при ошибке вернул %+v, want Config{}", in, c)
		}
	}
}

func TestParseConfigValid(t *testing.T) {
	tests := []struct {
		in   string
		want Config
	}{
		{"  \n{\"name\":\"a\",\"port\":0}\n\t ", Config{Name: "a", Port: 8080}},
		{`{"name":"a","port":65535,"tags":[]}`, Config{Name: "a", Port: 65535, Tags: []string{}}},
		{`{"name":"a","port":1,"debug":false,"tags":null}`, Config{Name: "a", Port: 1}},
		{`{"NAME":"a","Port":2,"DEBUG":true}`, Config{Name: "a", Port: 2, Debug: true}}, // регистр ключей не важен
	}
	for _, tt := range tests {
		got, err := ParseConfig([]byte(tt.in))
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseConfig(%q) = %+v, %v; want %+v, nil", tt.in, got, err, tt.want)
		}
	}
}

func TestEncodeConfigOmit(t *testing.T) {
	tests := []struct {
		in   Config
		want string
	}{
		{Config{Name: "api"}, `{"name":"api","port":0}`},
		{Config{Tags: []string{}}, `{"name":"","port":0}`},
		{Config{Name: "d", Port: 2, Debug: true}, `{"name":"d","port":2,"debug":true}`},
		{Config{Name: `"q"`, Port: 3, Tags: []string{"a b"}}, `{"name":"\"q\"","port":3,"tags":["a b"]}`},
	}
	for _, tt := range tests {
		got, err := EncodeConfig(tt.in)
		if err != nil || string(got) != tt.want {
			t.Errorf("EncodeConfig(%+v) = %s, %v; want %s", tt.in, got, err, tt.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := Config{Name: "svc", Port: 443, Tags: []string{"prod", "eu"}, Debug: true}
	b, err := EncodeConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseConfig(b)
	if err != nil || !reflect.DeepEqual(got, in) {
		t.Errorf("ParseConfig(EncodeConfig(c)) = %+v, %v; want %+v", got, err, in)
	}
}
