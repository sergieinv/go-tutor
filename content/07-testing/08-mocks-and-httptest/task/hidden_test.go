package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// *Client подходит туда, где ждут UserSource.
var _ UserSource = (*Client)(nil)

// recordingServer запоминает запросы и отвечает заданным хендлером.
type recordingServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
}

func newRecordingServer(t *testing.T, h http.HandlerFunc) *recordingServer {
	rs := &recordingServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.reqs = append(rs.reqs, r)
		rs.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *recordingServer) count() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return len(rs.reqs)
}

// last — последний запрос; пустой запрос, если запросов не было.
func (rs *recordingServer) last() *http.Request {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.reqs) == 0 {
		return &http.Request{URL: &url.URL{}, Header: http.Header{}}
	}
	return rs.reqs[len(rs.reqs)-1]
}

func jsonUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"login":"x","name":"Икс"}`)
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("http://example.test", nil)
	if c.HTTP == nil || c.HTTP.Timeout != 5*time.Second {
		t.Errorf("NewClient(url, nil).HTTP = %+v, want клиент с Timeout 5s", c.HTTP)
	}
	if c.HTTP == http.DefaultClient {
		t.Errorf("NewClient(url, nil) не должен менять и использовать http.DefaultClient")
	}
	hc := &http.Client{}
	if got := NewClient("http://example.test", hc); got.HTTP != hc {
		t.Errorf("NewClient(url, hc) должен использовать переданный hc")
	}
}

func TestClientRequest(t *testing.T) {
	rs := newRecordingServer(t, jsonUser)
	c := NewClient(rs.URL, rs.Client())
	u, err := c.User(context.Background(), "x")
	if err != nil || u != (User{Login: "x", Name: "Икс"}) {
		t.Fatalf("User(x) = %+v, %v", u, err)
	}
	r := rs.last()
	if r.Method != http.MethodGet {
		t.Errorf("метод %s, want GET", r.Method)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
	}
}

func TestClientEscapesLogin(t *testing.T) {
	rs := newRecordingServer(t, jsonUser)
	c := NewClient(rs.URL, rs.Client())
	for login, want := range map[string]string{
		"a b":    "/users/a%20b",
		"a/b":    "/users/a%2Fb",
		"q?x=1":  "/users/q%3Fx=1",
		"иван":   "/users/%D0%B8%D0%B2%D0%B0%D0%BD",
		"x#frag": "/users/x%23frag",
	} {
		if _, err := c.User(context.Background(), login); err != nil {
			t.Errorf("User(%q): %v", login, err)
			continue
		}
		if got := rs.last().URL.EscapedPath(); got != want {
			t.Errorf("User(%q): путь %q, want %q", login, got, want)
		}
		if q := rs.last().URL.RawQuery; q != "" {
			t.Errorf("User(%q): в запросе появилась строка запроса %q", login, q)
		}
	}
}

func TestClientTrailingSlash(t *testing.T) {
	rs := newRecordingServer(t, jsonUser)
	c := NewClient(rs.URL+"/", rs.Client())
	if _, err := c.User(context.Background(), "x"); err != nil {
		t.Fatalf("User(x): %v", err)
	}
	if got := rs.last().URL.Path; got != "/users/x" {
		t.Errorf("BaseURL со слешем на конце: путь %q, want /users/x", got)
	}
}

func TestClientEmptyLogin(t *testing.T) {
	rs := newRecordingServer(t, jsonUser)
	c := NewClient(rs.URL, rs.Client())
	if _, err := c.User(context.Background(), ""); err == nil {
		t.Error("User(\"\"): ожидалась ошибка")
	}
	if n := rs.count(); n != 0 {
		t.Errorf("User(\"\") отправил %d запрос(ов), want 0", n)
	}
}

func TestClientStatuses(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		notFound bool
		contains string
	}{
		{"404", 404, "not found", true, ""},
		{"500", 500, "oops", false, "500"},
		{"403", 403, "", false, "403"},
		{"201 не 200", 201, `{"login":"x"}`, false, "201"},
		{"битый JSON", 200, `{"login":`, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			})
			u, err := NewClient(rs.URL, rs.Client()).User(context.Background(), "x")
			if err == nil {
				t.Fatalf("User = %+v, nil; want ошибку", u)
			}
			if u != (User{}) {
				t.Errorf("при ошибке User = %+v, want нулевое значение", u)
			}
			if errors.Is(err, ErrNotFound) != tt.notFound {
				t.Errorf("errors.Is(%v, ErrNotFound) = %v, want %v", err, !tt.notFound, tt.notFound)
			}
			if tt.contains != "" && !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("текст ошибки %q не содержит код %s", err, tt.contains)
			}
		})
	}
}

func TestClientContext(t *testing.T) {
	release := make(chan struct{})
	rs := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	defer close(release)
	c := NewClient(rs.URL, rs.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.User(ctx, "slow")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("User с истёкшим контекстом: ошибка %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("User не прервался по контексту: %v", d)
	}

	canceled, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := c.User(canceled, "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("User с отменённым контекстом: ошибка %v, want context.Canceled", err)
	}
}

// fakeSource — потокобезопасный фейк с настраиваемой ошибкой.
type fakeSource struct {
	mu    sync.Mutex
	calls map[string]int
	fail  map[string]error
	ver   int
}

func newFakeSource() *fakeSource {
	return &fakeSource{calls: map[string]int{}, fail: map[string]error{}}
}

func (f *fakeSource) User(_ context.Context, login string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[login]++
	if err := f.fail[login]; err != nil {
		return User{}, err
	}
	f.ver++
	return User{Login: login, Name: fmt.Sprintf("v%d", f.ver)}, nil
}

func (f *fakeSource) n(login string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[login]
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestCacheBoundary(t *testing.T) {
	clk := &clock{now: time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)}
	src := newFakeSource()
	c := NewCache(src, 10*time.Second, clk.Now)
	ctx := context.Background()

	first, _ := c.User(ctx, "a")
	clk.Add(10*time.Second - time.Nanosecond)
	if u, _ := c.User(ctx, "a"); u != first || src.n("a") != 1 {
		t.Errorf("за наносекунду до ttl: %+v, вызовов %d; want кеш %+v и 1 вызов", u, src.n("a"), first)
	}
	clk.Add(time.Nanosecond)
	second, _ := c.User(ctx, "a")
	if second == first || src.n("a") != 2 {
		t.Errorf("ровно через ttl: %+v, вызовов %d; want свежий ответ и 2 вызова", second, src.n("a"))
	}
	// Время записи — момент обновления, а не первого запроса.
	clk.Add(9 * time.Second)
	if u, _ := c.User(ctx, "a"); u != second || src.n("a") != 2 {
		t.Errorf("после обновления кеш должен жить ttl от времени обновления: %+v, вызовов %d", u, src.n("a"))
	}
}

func TestCachePerLogin(t *testing.T) {
	clk := &clock{now: time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)}
	src := newFakeSource()
	c := NewCache(src, time.Minute, clk.Now)
	ctx := context.Background()
	a, _ := c.User(ctx, "a")
	b, _ := c.User(ctx, "b")
	if a.Login != "a" || b.Login != "b" {
		t.Errorf("кеш перепутал логины: %+v, %+v", a, b)
	}
	c.User(ctx, "a")
	c.User(ctx, "b")
	if src.n("a") != 1 || src.n("b") != 1 {
		t.Errorf("вызовов: a=%d b=%d, want по одному", src.n("a"), src.n("b"))
	}
}

func TestCacheErrorsNotCached(t *testing.T) {
	clk := &clock{now: time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)}
	src := newFakeSource()
	boom := errors.New("сеть недоступна")
	src.fail["a"] = boom
	c := NewCache(src, time.Minute, clk.Now)
	ctx := context.Background()

	if u, err := c.User(ctx, "a"); !errors.Is(err, boom) || u != (User{}) {
		t.Errorf("User при ошибке источника = %+v, %v; want нулевой User и ошибку источника", u, err)
	}
	if _, err := c.User(ctx, "a"); !errors.Is(err, boom) || src.n("a") != 2 {
		t.Errorf("ошибка не должна кешироваться: вызовов %d, want 2 (err=%v)", src.n("a"), err)
	}
	src.mu.Lock()
	delete(src.fail, "a")
	src.mu.Unlock()
	if u, err := c.User(ctx, "a"); err != nil || u.Login != "a" {
		t.Errorf("после восстановления источника: %+v, %v", u, err)
	}

	// ErrNotFound тоже не кешируется.
	src.fail["ghost"] = ErrNotFound
	c.User(ctx, "ghost")
	c.User(ctx, "ghost")
	if src.n("ghost") != 2 {
		t.Errorf("ErrNotFound не должен кешироваться: вызовов %d, want 2", src.n("ghost"))
	}
}

func TestCachePassesContext(t *testing.T) {
	type key struct{}
	var got atomic.Value
	src := sourceFunc(func(ctx context.Context, login string) (User, error) {
		got.Store(ctx.Value(key{}))
		return User{Login: login}, nil
	})
	c := NewCache(src, time.Minute, time.Now)
	ctx := context.WithValue(context.Background(), key{}, "marker")
	c.User(ctx, "a")
	if got.Load() != "marker" {
		t.Errorf("Cache должен передавать контекст вызова в источник")
	}
}

type sourceFunc func(ctx context.Context, login string) (User, error)

func (f sourceFunc) User(ctx context.Context, login string) (User, error) { return f(ctx, login) }

func TestCacheConcurrent(t *testing.T) {
	clk := &clock{now: time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)}
	src := newFakeSource()
	c := NewCache(src, time.Minute, clk.Now)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			login := fmt.Sprintf("u%d", i%5)
			if i%10 == 0 {
				clk.Add(time.Second)
			}
			u, err := c.User(context.Background(), login)
			if err != nil || u.Login != login {
				t.Errorf("User(%s) = %+v, %v", login, u, err)
			}
		})
	}
	wg.Wait()
}

func TestCacheOverClient(t *testing.T) {
	rs := newRecordingServer(t, jsonUser)
	clk := &clock{now: time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)}
	c := NewCache(NewClient(rs.URL, rs.Client()), time.Minute, clk.Now)
	for range 3 {
		if _, err := c.User(context.Background(), "x"); err != nil {
			t.Fatal(err)
		}
	}
	if n := rs.count(); n != 1 {
		t.Errorf("Cache поверх Client: %d HTTP-запросов, want 1", n)
	}
}
