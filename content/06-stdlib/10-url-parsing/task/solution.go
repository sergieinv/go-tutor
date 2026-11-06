package task

import (
	"fmt"
	"net/url"
	"strings"
)

var defaultPorts = map[string]string{"http": "80", "https": "443"}

// Normalize приводит http(s)-адрес к каноническому виду: схема и хост
// в нижнем регистре, без порта по умолчанию, путь не пустой,
// параметры отсортированы, без фрагмента и данных пользователя.
func Normalize(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	def, ok := defaultPorts[u.Scheme]
	if !ok {
		return "", fmt.Errorf("неподдерживаемая схема %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("пустой хост в %q", raw)
	}
	if port := u.Port(); port != "" && port != def {
		host += ":" + port
	}
	u.Host = host
	if u.Path == "" {
		u.Path = "/"
	}
	// Encode сортирует ключи; пустой результат — URL без "?".
	u.RawQuery = u.Query().Encode()
	u.ForceQuery = false
	u.Fragment, u.RawFragment = "", ""
	u.User = nil
	return u.String(), nil
}
