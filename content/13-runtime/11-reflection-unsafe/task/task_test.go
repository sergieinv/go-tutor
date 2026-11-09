package task

import (
	"errors"
	"testing"
	"time"
)

type serverConfig struct {
	Host    string        `env:"HOST"`
	Port    int           `env:"PORT,required"`
	Debug   bool          `env:"DEBUG"`
	Timeout time.Duration `env:"TIMEOUT"`
	Name    string        // без тега — не трогаем
}

func TestLoadAllTypes(t *testing.T) {
	cfg := serverConfig{Host: "localhost", Name: "keep"}
	err := Load(&cfg, map[string]string{
		"PORT":    "8080",
		"DEBUG":   "true",
		"TIMEOUT": "1m30s",
		"Name":    "ignored",
	})
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	want := serverConfig{Host: "localhost", Port: 8080, Debug: true, Timeout: 90 * time.Second, Name: "keep"}
	if cfg != want {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	var cfg serverConfig
	err := Load(&cfg, map[string]string{"HOST": "example.com"})
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("Load без PORT = %v, want ErrMissing", err)
	}
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "Port" || fe.Key != "PORT" {
		t.Errorf("ошибка должна быть *FieldError{Field: Port, Key: PORT}, получили %#v", err)
	}
}

func TestLoadInvalidTarget(t *testing.T) {
	var cfg serverConfig
	if err := Load(cfg, nil); !errors.Is(err, ErrInvalidTarget) {
		t.Errorf("Load(структура по значению) = %v, want ErrInvalidTarget", err)
	}
}
