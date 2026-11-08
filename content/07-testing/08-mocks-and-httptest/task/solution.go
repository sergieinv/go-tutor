package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{BaseURL: strings.TrimSuffix(baseURL, "/"), HTTP: hc}
}

// User запрашивает GET {BaseURL}/users/{login}.
func (c *Client) User(ctx context.Context, login string) (User, error) {
	if login == "" {
		return User{}, errors.New("users: пустой login")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/users/"+url.PathEscape(login), nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return User{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return User{}, fmt.Errorf("%s: %w", login, ErrNotFound)
	default:
		return User{}, fmt.Errorf("users: неожиданный статус %d", resp.StatusCode)
	}
	var u User
	// Ответ чужого сервиса читаем с лимитом размера.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&u); err != nil {
		return User{}, fmt.Errorf("users: разбор ответа: %w", err)
	}
	return u, nil
}

// UserSource — всё, что нужно кешу от источника.
type UserSource interface {
	User(ctx context.Context, login string) (User, error)
}

// Cache кеширует ответы источника на ttl.
type Cache struct {
	src UserSource
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	user User
	at   time.Time
}

// NewCache создаёт кеш поверх src; now — источник текущего времени.
func NewCache(src UserSource, ttl time.Duration, now func() time.Time) *Cache {
	return &Cache{src: src, ttl: ttl, now: now, entries: map[string]entry{}}
}

// User возвращает пользователя из кеша или из источника.
func (c *Cache) User(ctx context.Context, login string) (User, error) {
	c.mu.Lock()
	e, ok := c.entries[login]
	c.mu.Unlock()
	if ok && c.now().Sub(e.at) < c.ttl {
		return e.user, nil
	}
	// Источник вызываем без блокировки: медленный запрос
	// не задерживает чтение других логинов из кеша.
	u, err := c.src.User(ctx, login)
	if err != nil {
		return User{}, err
	}
	c.mu.Lock()
	c.entries[login] = entry{u, c.now()}
	c.mu.Unlock()
	return u, nil
}
