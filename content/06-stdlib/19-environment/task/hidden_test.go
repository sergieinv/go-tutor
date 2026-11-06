package task

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadEmptyMeansDefault(t *testing.T) {
	setEnv(t, map[string]string{"APP_ADDR": "", "APP_DEBUG": "", "APP_TIMEOUT": "", "APP_WORKERS": ""})
	got, err := Load()
	want := Config{Addr: ":8080", Timeout: 5 * time.Second, Workers: 4, Tags: []string{"default"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, %v; want %+v, nil — пустые значения равносильны отсутствию", got, err, want)
	}
}

func TestLoadTagsSetButEmpty(t *testing.T) {
	for _, v := range []string{"", " , ,", " "} {
		setEnv(t, map[string]string{"APP_TAGS": v})
		got, err := Load()
		if err != nil || len(got.Tags) != 0 {
			t.Errorf("APP_TAGS=%q: Tags = %q, %v; want пустой список", v, got.Tags, err)
		}
	}
}

func TestLoadTagsTrim(t *testing.T) {
	setEnv(t, map[string]string{"APP_TAGS": " a ,,b,  c  ,"})
	got, err := Load()
	if err != nil || !reflect.DeepEqual(got.Tags, []string{"a", "b", "c"}) {
		t.Errorf("Tags = %q, %v; want [a b c]", got.Tags, err)
	}
}

func TestLoadDebugForms(t *testing.T) {
	for v, want := range map[string]bool{"1": true, "TRUE": true, "f": false, "0": false} {
		setEnv(t, map[string]string{"APP_DEBUG": v})
		got, err := Load()
		if err != nil || got.Debug != want {
			t.Errorf("APP_DEBUG=%q: Debug = %v, %v; want %v", v, got.Debug, err, want)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct{ key, val string }{
		{"APP_DEBUG", "yes"},
		{"APP_TIMEOUT", "5"},
		{"APP_TIMEOUT", "0s"},
		{"APP_TIMEOUT", "-1s"},
		{"APP_WORKERS", "0"},
		{"APP_WORKERS", "-3"},
		{"APP_WORKERS", "2.5"},
	}
	for _, tt := range tests {
		setEnv(t, map[string]string{tt.key: tt.val})
		got, err := Load()
		if err == nil || !strings.Contains(err.Error(), tt.key) {
			t.Errorf("%s=%q: Load() вернул ошибку %v, want упоминание %s", tt.key, tt.val, err, tt.key)
		}
		if !reflect.DeepEqual(got, Config{}) {
			t.Errorf("%s=%q: при ошибке Load() вернул %+v, want Config{}", tt.key, tt.val, got)
		}
	}
}
