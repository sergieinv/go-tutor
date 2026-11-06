package task

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

var appVars = []string{"APP_ADDR", "APP_DEBUG", "APP_TIMEOUT", "APP_WORKERS", "APP_TAGS"}

// setEnv задаёт переменные на время теста; остальные APP_* удаляет.
// t.Setenv перед Unsetenv нужен, чтобы после теста вернуть прежнее значение.
func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, k := range appVars {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, nil)
	got, err := Load()
	want := Config{Addr: ":8080", Timeout: 5 * time.Second, Workers: 4, Tags: []string{"default"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestLoadFromEnv(t *testing.T) {
	setEnv(t, map[string]string{
		"APP_ADDR":    "0.0.0.0:9000",
		"APP_DEBUG":   "true",
		"APP_TIMEOUT": "1m30s",
		"APP_WORKERS": "16",
		"APP_TAGS":    "api, v2",
	})
	got, err := Load()
	want := Config{Addr: "0.0.0.0:9000", Debug: true, Timeout: 90 * time.Second, Workers: 16, Tags: []string{"api", "v2"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestLoadBadWorkers(t *testing.T) {
	setEnv(t, map[string]string{"APP_WORKERS": "много"})
	got, err := Load()
	if err == nil || !strings.Contains(err.Error(), "APP_WORKERS") {
		t.Fatalf("Load() вернул ошибку %v, want упоминание APP_WORKERS", err)
	}
	if !reflect.DeepEqual(got, Config{}) {
		t.Errorf("при ошибке Load() вернул %+v, want Config{}", got)
	}
}
