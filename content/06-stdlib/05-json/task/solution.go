package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Config — конфигурация сервиса.
type Config struct {
	Name  string   `json:"name"`
	Port  int      `json:"port"`
	Tags  []string `json:"tags,omitempty"`
	Debug bool     `json:"debug,omitempty"`
}

// ParseConfig строго разбирает JSON-объект конфигурации.
func ParseConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	// Decode читает одно значение; после него должен быть только EOF.
	if err := dec.Decode(&json.RawMessage{}); err != io.EOF {
		return Config{}, errors.New("config: лишние данные после объекта")
	}
	if c.Name == "" {
		return Config{}, errors.New("config: не задан name")
	}
	if c.Port == 0 {
		c.Port = 8080
	}
	if c.Port < 1 || c.Port > 65535 {
		return Config{}, fmt.Errorf("config: порт %d вне диапазона", c.Port)
	}
	return c, nil
}

// EncodeConfig кодирует конфигурацию в компактный JSON.
func EncodeConfig(c Config) ([]byte, error) {
	return json.Marshal(c)
}
