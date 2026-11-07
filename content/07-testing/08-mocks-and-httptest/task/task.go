package task

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// User — профиль пользователя из API.
type User struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// ErrNotFound — API ответил 404.
var ErrNotFound = errors.New("users: пользователь не найден")

// Client — клиент API пользователей.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient создаёт клиент; hc == nil — клиент с таймаутом 5 секунд.
func NewClient(baseURL string, hc *http.Client) *Client {
	// TODO: реализуйте функцию
	return &Client{}
}

// User запрашивает GET {BaseURL}/users/{login}.
func (c *Client) User(ctx context.Context, login string) (User, error) {
	// TODO: реализуйте метод
	return User{}, nil
}

// UserSource — всё, что нужно кешу от источника.
type UserSource interface {
	User(ctx context.Context, login string) (User, error)
}

// Cache кеширует ответы источника на ttl.
type Cache struct {
	// TODO: поля
}

// NewCache создаёт кеш поверх src; now — источник текущего времени.
func NewCache(src UserSource, ttl time.Duration, now func() time.Time) *Cache {
	// TODO: реализуйте функцию
	return &Cache{}
}

// User возвращает пользователя из кеша или из источника.
func (c *Cache) User(ctx context.Context, login string) (User, error) {
	// TODO: реализуйте метод
	return User{}, nil
}
