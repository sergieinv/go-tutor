package load

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Client — HTTP-клиент к REST «Заказов», который записывает каждый запрос
// в Stats.
type Client struct {
	base  string
	http  *http.Client
	stats *Stats
}

// NewClient создаёт клиента. timeout — на один запрос.
func NewClient(base string, timeout time.Duration, stats *Stats) *Client {
	tr := &http.Transport{
		// Генератор держит много соединений к одному хосту: без этого
		// стандартные 2 простаивающих соединения на хост превратили бы
		// нагрузку в постоянное открытие новых TCP-соединений.
		MaxIdleConns:        1024,
		MaxIdleConnsPerHost: 1024,
		IdleConnTimeout:     30 * time.Second,
		DialContext:         (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
	}
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: timeout, Transport: tr}, stats: stats}
}

// maxResponse — больше этого ответ не читается (защита генератора).
const maxResponse = 1 << 20

// do выполняет запрос, записывает исход и задержку (от intended) и
// возвращает код и тело. Ошибка — только транспортная.
func (c *Client) do(ctx context.Context, op string, intended time.Time, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	if intended.IsZero() {
		intended = time.Now()
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.stats.Record(op, intended, time.Now(), transportOutcome(err))
		return 0, nil, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	resp.Body.Close()
	end := time.Now()
	if err != nil {
		c.stats.Record(op, intended, end, transportOutcome(err))
		return 0, nil, err
	}
	c.stats.Record(op, intended, end, strconv.Itoa(resp.StatusCode))
	return resp.StatusCode, data, nil
}

func transportOutcome(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded),
		errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	}
	return "conn_error"
}

// Scenario — что делает один «пользователь» за один заход.
type Scenario struct {
	Name   string
	Weight int
	// Run выполняет сценарий. intended — запланированное время начала
	// (первый запрос считает задержку от него).
	Run func(ctx context.Context, c *Client, r *rand.Rand, intended time.Time)
}

// Options — параметры сценариев.
type Options struct {
	// Customers — покупатели 1..Customers.
	Customers int
	// Products — товары каталога 1..Products, которые запрашивает catalog.
	Products int
	// MissingRatio — доля запросов catalog к несуществующим товарам
	// (проверка отрицательного кеша).
	MissingRatio float64
	// OrderProducts — какие товары заказывать (по одной штуке); пусто —
	// товар 4 «Кабель USB-C» с миллионом на складе. Заказ сразу
	// отменяется: так остатки не убывают и прогон можно повторять.
	OrderProducts []int64
	// RYW — после создания заказа проверить read-your-writes: история без
	// токена и с токеном (два лишних запроса orders.list_ryw).
	RYW bool
}

// Scenarios возвращает сценарии по именам: catalog, orders, history.
func Scenarios(o Options, stats *Stats) map[string]Scenario {
	return map[string]Scenario{
		"catalog": {Name: "catalog", Run: func(ctx context.Context, c *Client, r *rand.Rand, intended time.Time) {
			id := 1 + r.IntN(max(o.Products, 1))
			if r.Float64() < o.MissingRatio {
				id = 100000 + r.IntN(1000) // таких товаров нет
			}
			c.do(ctx, "catalog.get", intended, http.MethodGet, "/v1/products/"+strconv.Itoa(id), nil)
		}},
		"history": {Name: "history", Run: func(ctx context.Context, c *Client, r *rand.Rand, intended time.Time) {
			customer := 1 + r.IntN(max(o.Customers, 1))
			c.do(ctx, "orders.list", intended, http.MethodGet,
				fmt.Sprintf("/v1/orders?customer_id=%d&page_size=10", customer), nil)
		}},
		"orders": {Name: "orders", Run: func(ctx context.Context, c *Client, r *rand.Rand, intended time.Time) {
			runOrder(ctx, c, r, intended, o, stats)
		}},
	}
}

// runOrder — создать заказ, (проверить историю), отменить.
func runOrder(ctx context.Context, c *Client, r *rand.Rand, intended time.Time, o Options, stats *Stats) {
	customer := int64(1 + r.IntN(max(o.Customers, 1)))
	products := o.OrderProducts
	if len(products) == 0 {
		products = []int64{4}
	}
	product := products[r.IntN(len(products))]
	code, body, err := c.do(ctx, "orders.create", intended, http.MethodPost, "/v1/orders", map[string]any{
		"customer_id": customer,
		"items":       []map[string]any{{"product_id": product, "quantity": 1}},
	})
	if err != nil || code != http.StatusOK {
		return
	}
	var created struct {
		Order struct {
			OrderID string `json:"order_id"`
		} `json:"order"`
		ConsistencyToken string `json:"consistency_token"`
	}
	if json.Unmarshal(body, &created) != nil || created.Order.OrderID == "" {
		return
	}
	if o.RYW {
		noTok := !orderVisible(ctx, c, customer, created.Order.OrderID, "")
		withTok := !orderVisible(ctx, c, customer, created.Order.OrderID, created.ConsistencyToken)
		stats.add(func(s *Stats) {
			s.RYWChecks++
			if noTok {
				s.StaleNoToken++
			}
			if withTok {
				s.StaleWithToken++
			}
		})
	}
	// Отмена возвращает товар на склад (и нагружает CancelOrder): остатки
	// не убывают от прогона к прогону.
	c.do(ctx, "orders.cancel", time.Time{}, http.MethodPost, "/v1/orders/"+created.Order.OrderID+":cancel", map[string]any{})
}

// orderVisible — есть ли заказ на первой странице истории покупателя.
func orderVisible(ctx context.Context, c *Client, customer int64, orderID, token string) bool {
	q := url.Values{"customer_id": {strconv.FormatInt(customer, 10)}, "page_size": {"5"}}
	if token != "" {
		q.Set("consistency_token", token)
	}
	code, body, err := c.do(ctx, "orders.list_ryw", time.Time{}, http.MethodGet, "/v1/orders?"+q.Encode(), nil)
	if err != nil || code != http.StatusOK {
		return true // ошибку не считаем «устаревшим чтением»: она видна в исходах
	}
	var list struct {
		Orders []struct {
			OrderID string `json:"order_id"`
		} `json:"orders"`
	}
	if json.Unmarshal(body, &list) != nil {
		return true
	}
	return slices.ContainsFunc(list.Orders, func(o struct {
		OrderID string `json:"order_id"`
	}) bool {
		return o.OrderID == orderID
	})
}

// ParseMix разбирает смесь «catalog=6,orders=1,history=3».
func ParseMix(s string, all map[string]Scenario) ([]Scenario, error) {
	var mix []Scenario
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, w, ok := strings.Cut(part, "=")
		weight := 1
		if ok {
			n, err := strconv.Atoi(w)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("mix %q: weight must be a non-negative integer", part)
			}
			weight = n
		}
		sc, ok := all[name]
		if !ok {
			return nil, fmt.Errorf("mix: unknown scenario %q (want catalog, orders, history)", name)
		}
		if weight > 0 {
			sc.Weight = weight
			mix = append(mix, sc)
		}
	}
	if len(mix) == 0 {
		return nil, errors.New("mix: no scenarios")
	}
	return mix, nil
}

// pick выбирает сценарий по весам.
func pick(mix []Scenario, r *rand.Rand) Scenario {
	total := 0
	for _, s := range mix {
		total += s.Weight
	}
	n := r.IntN(total)
	for _, s := range mix {
		if n < s.Weight {
			return s
		}
		n -= s.Weight
	}
	return mix[len(mix)-1]
}
