// Сервис конвертации валют: ходит во внешний API за
// курсом, кеширует его на минуту и отдаёт результат по
// HTTP. Всё, что мешает тестам — сеть и часы, — приходит
// снаружи, поэтому тесты подставляют свои реализации.

package rates

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Интерфейс объявляет потребитель — и ровно с теми
// методами, которые ему нужны. Реализации о нём не знают.
type RateSource interface {
	Rate(ctx context.Context, cur string) (float64, error)
}

// Настоящий источник — клиент внешнего API. Адрес и
// `*http.Client` внедряются: в коде это клиент с таймаутом
// (`&http.Client{Timeout: 5 * time.Second}`), в тесте —
// клиент `httptest.Server`.
type HTTPSource struct {
	BaseURL string
	Client  *http.Client
}

func (s *HTTPSource) Rate(ctx context.Context, cur string) (float64, error) {
	u := s.BaseURL + "/rates/" + url.PathEscape(cur)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("rates: статус %d", resp.StatusCode)
	}
	var body struct {
		Rate float64 `json:"rate"`
	}
	err = json.NewDecoder(resp.Body).Decode(&body)
	return body.Rate, err
}

// Часы — просто функция: в коде `time.Now`, в тесте —
// функция, которая возвращает то время, какое скажем.
type Converter struct {
	src   RateSource
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	rate float64
	at   time.Time
}

func NewConverter(src RateSource, now func() time.Time) *Converter {
	return &Converter{src: src, now: now, cache: map[string]cached{}}
}

func (c *Converter) Convert(ctx context.Context, amount float64, cur string) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[cur]
	if !ok || c.now().Sub(e.at) >= time.Minute {
		rate, err := c.src.Rate(ctx, cur)
		if err != nil {
			return 0, err
		}
		e = cached{rate, c.now()}
		c.cache[cur] = e
	}
	return amount * e.rate, nil
}

// Хендлер — обычный `http.Handler`: его можно вызвать
// напрямую, без сервера и сети.
func (c *Converter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	amount, err := strconv.ParseFloat(r.URL.Query().Get("amount"), 64)
	if err != nil {
		http.Error(w, "amount: ожидается число", http.StatusBadRequest)
		return
	}
	res, err := c.Convert(r.Context(), amount, r.URL.Query().Get("cur"))
	if err != nil {
		http.Error(w, "курс недоступен", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]float64{"result": res})
}
