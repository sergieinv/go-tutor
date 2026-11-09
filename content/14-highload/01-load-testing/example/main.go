// Мини-генератор нагрузки против медленного сервера на loopback.
// Задержки от запуска к запуску разные, поэтому печатаем только
// то, что от машины не зависит: перцентили синтетического набора,
// число запросов и сравнения, которые гарантирует устройство теста.
package main

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"time"
)

// Перцентиль методом ближайшего ранга: p-й перцентиль — значение
// с номером ⌈p/100·n⌉ в отсортированном наборе. Никакой
// интерполяции: результат — всегда одно из измеренных значений.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	k := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[min(max(k, 1), len(sorted))-1]
}

func mean(ds []time.Duration) time.Duration {
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return sum / time.Duration(len(ds))
}

func main() {
	// **Среднее прячет хвост.** 985 быстрых ответов, 10 медленных
	// и 5 совсем медленных: среднее выглядит прилично, но каждый
	// сотый пользователь ждёт 200 мс, а каждый тысячный — 2 с.
	var synth []time.Duration
	for i := range 1000 {
		switch {
		case i < 985:
			synth = append(synth, 10*time.Millisecond)
		case i < 995:
			synth = append(synth, 200*time.Millisecond)
		default:
			synth = append(synth, 2*time.Second)
		}
	}
	slices.Sort(synth)
	fmt.Println("синтетика: среднее", mean(synth))
	fmt.Println("p50", percentile(synth, 50), "p99", percentile(synth, 99),
		"p99.9", percentile(synth, 99.9), "max", synth[len(synth)-1])

	// **Медленный сервер**: обрабатывает один запрос за раз, 10 мс
	// на запрос, то есть не больше 100 запросов в секунду.
	var mu sync.Mutex
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		time.Sleep(10 * time.Millisecond)
		mu.Unlock()
		io.WriteString(w, "ok")
	}))
	srv.Config.ReadHeaderTimeout = 2 * time.Second
	srv.Start()
	defer srv.Close()
	client := &http.Client{
		Transport: &http.Transport{MaxIdleConnsPerHost: 64},
		Timeout:   10 * time.Second,
	}
	send := func() error {
		resp, err := client.Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}

	// Расписание: 30 запросов раз в 2 мс — 500 запросов в секунду,
	// в пять раз больше, чем сервер способен обслужить.
	const n, interval = 30, 2 * time.Millisecond

	// **Открытая модель**: запрос уходит по расписанию, что бы ни
	// происходило с предыдущими, — каждый в своей горутине. Так
	// приходят пользователи в реальности: им всё равно, что сервер
	// не успевает. Задержку меряем от запланированного момента.
	start := time.Now()
	open := make([]time.Duration, n)
	var openErrs int
	var resMu sync.Mutex // свой мьютекс: серверный здесь исказил бы замер
	var wg sync.WaitGroup
	for i := range n {
		intended := start.Add(time.Duration(i) * interval)
		time.Sleep(time.Until(intended))
		wg.Go(func() {
			err := send()
			resMu.Lock()
			defer resMu.Unlock()
			if err != nil {
				openErrs++
			}
			open[i] = time.Since(intended)
		})
	}
	wg.Wait()
	slices.Sort(open)
	fmt.Println("открытая модель: отправлено", n, "ошибок", openErrs)

	// **Закрытая модель**: один «пользователь» шлёт следующий
	// запрос только после ответа на предыдущий. Он хочет идти по
	// тому же расписанию, но медленный сервер его тормозит —
	// генератор сам снижает нагрузку именно тогда, когда она важна.
	start = time.Now()
	var fromSend, fromPlan []time.Duration
	var closedErrs, sentInWindow int
	for i := range n {
		intended := start.Add(time.Duration(i) * interval)
		time.Sleep(time.Until(intended))
		sent := time.Now()
		if sent.Sub(start) < n*interval {
			sentInWindow++
		}
		if err := send(); err != nil {
			closedErrs++
		}
		fromSend = append(fromSend, time.Since(sent))
		fromPlan = append(fromPlan, time.Since(intended))
	}
	slices.Sort(fromSend)
	slices.Sort(fromPlan)
	fmt.Println("закрытая модель: отправлено", n, "ошибок", closedErrs)
	fmt.Println("за время расписания закрытая успела меньше:", sentInWindow < n)

	// **Coordinated omission.** Если мерить от фактической отправки,
	// закрытая модель видит только 10 мс обработки: ожидание в
	// очереди «съедено» генератором, который просто не отправлял.
	// От запланированного момента видно то, что видят пользователи.
	p99Send, p99Plan := percentile(fromSend, 99), percentile(fromPlan, 99)
	fmt.Println("p99 от отправки меньше p99 от расписания:", p99Send < p99Plan)
	// Сервер обработает 30 запросов не быстрее чем за 300 мс, а
	// последний запланирован на 58 мс — значит, его задержка от
	// расписания не меньше 242 мс в обеих честных метриках.
	fmt.Println("p99 открытой и p99 от расписания больше 200 мс:",
		percentile(open, 99) > 200*time.Millisecond, p99Plan > 200*time.Millisecond)
}
