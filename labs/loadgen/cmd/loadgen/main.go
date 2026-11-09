// Команда loadgen — генератор нагрузки для «Заказов» (модуль 14).
//
//	loadgen run [флаги]       один прогон: открытая или закрытая модель, смесь сценариев
//	loadgen analyze [флаги]   нагрузочный разбор: база → кеш → реплика → перегрузка
//
// В docker compose (профиль loadtest, сеть default):
//
//	docker compose --profile labs run --rm loadgen analyze
//	docker compose --profile labs run --rm loadgen run -rate 300 -duration 30s
//
// С машины — те же команды через go run с адресами 127.0.0.1 (см. README).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gotutor/labs/loadgen/internal/analyze"
	"gotutor/labs/loadgen/internal/load"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "loadgen:", err)
		os.Exit(1)
	}
}

const usage = `usage: loadgen <command> [flags]

commands:
  run       one load run (open or closed model), prints a report
  analyze   load analysis of the orders service: baseline → catalog cache →
            replica reads → CreateOrder overload, with a summary table

loadgen <command> -h — flags of the command`

// run разбирает подкоманду. getenv — для адресов по умолчанию
// (LOADGEN_TARGET, LOADGEN_OPS), которые задаёт compose.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stdout, usage)
		return errors.New("command required")
	}
	switch args[0] {
	case "run":
		return runLoad(ctx, args[1:], getenv, stdout)
	case "analyze":
		return runAnalyze(ctx, args[1:], getenv, stdout)
	case "-h", "-help", "--help", "help":
		fmt.Fprintln(stdout, usage)
		return nil
	}
	fmt.Fprintln(stdout, usage)
	return fmt.Errorf("unknown command %q", args[0])
}

// common — флаги обеих команд.
type common struct {
	target    string
	customers int
	products  int
	missing   float64
	orderProd string
	timeout   time.Duration
	seed      uint64
}

func (c *common) register(fs *flag.FlagSet, getenv func(string) string) {
	fs.StringVar(&c.target, "target", orDefault(getenv("LOADGEN_TARGET"), "http://127.0.0.1:8090"), "REST «Заказов»")
	fs.IntVar(&c.customers, "customers", 1000, "покупатели 1..N")
	fs.IntVar(&c.products, "products", 3, "товары каталога 1..N для сценария catalog")
	fs.Float64Var(&c.missing, "missing", 0.05, "доля запросов к несуществующим товарам (отрицательный кеш)")
	fs.StringVar(&c.orderProd, "order-products", "4", "какие товары заказывать: 4 — «Кабель USB-C» с миллионом на складе (заказ сразу отменяется)")
	fs.DurationVar(&c.timeout, "timeout", 5*time.Second, "таймаут одного запроса")
	fs.Uint64Var(&c.seed, "seed", uint64(time.Now().UnixNano()), "зерно случайности")
}

func (c *common) options(ryw bool) (load.Options, error) {
	o := load.Options{Customers: c.customers, Products: c.products, MissingRatio: c.missing, RYW: ryw}
	for p := range strings.SplitSeq(c.orderProd, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || id <= 0 {
			return o, fmt.Errorf("-order-products %q: want comma-separated product ids", c.orderProd)
		}
		o.OrderProducts = append(o.OrderProducts, id)
	}
	if c.customers < 1 || c.products < 1 || c.missing < 0 || c.missing > 1 || c.timeout <= 0 {
		return o, errors.New("-customers, -products ≥ 1; -missing in [0, 1]; -timeout > 0")
	}
	return o, nil
}

func runLoad(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("loadgen run", flag.ContinueOnError)
	fs.SetOutput(stdout)
	var c common
	c.register(fs, getenv)
	model := fs.String("model", "open", "open — заданная частота прихода; closed — N пользователей с паузой")
	rate := fs.Float64("rate", 100, "open: сценариев в секунду")
	maxInFlight := fs.Int("max-in-flight", 10000, "open: предохранитель генератора — больше сценариев одновременно не запускать")
	users := fs.Int("users", 20, "closed: виртуальных пользователей")
	think := fs.Duration("think", 100*time.Millisecond, "closed: пауза между сценариями пользователя (±50%)")
	duration := fs.Duration("duration", 15*time.Second, "длительность прогона")
	mixSpec := fs.String("mix", "catalog=6,orders=1,history=3", "смесь сценариев с весами")
	ryw := fs.Bool("ryw", false, "orders: проверять read-your-writes (история без токена и с токеном)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	opts, err := c.options(*ryw)
	if err != nil {
		return err
	}
	stats := load.NewStats()
	mix, err := load.ParseMix(*mixSpec, load.Scenarios(opts, stats))
	if err != nil {
		return err
	}
	if *duration <= 0 {
		return errors.New("-duration must be positive")
	}
	client := load.NewClient(c.target, c.timeout, stats)
	switch *model {
	case "open":
		if *rate <= 0 || *maxInFlight <= 0 {
			return errors.New("-rate and -max-in-flight must be positive")
		}
		fmt.Fprintf(stdout, "открытая модель: %.0f сценариев/с, %s, смесь %q → %s\n", *rate, *duration, *mixSpec, c.target)
		load.RunOpen(ctx, client, mix, load.OpenConfig{Rate: *rate, Duration: *duration, MaxInFlight: *maxInFlight, Seed: c.seed}, stats)
	case "closed":
		if *users <= 0 || *think < 0 {
			return errors.New("-users must be positive, -think non-negative")
		}
		fmt.Fprintf(stdout, "закрытая модель: %d пользователей, пауза %s, %s, смесь %q → %s\n", *users, *think, *duration, *mixSpec, c.target)
		load.RunClosed(ctx, client, mix, load.ClosedConfig{Users: *users, Think: *think, Duration: *duration, Seed: c.seed}, stats)
	default:
		return fmt.Errorf("-model %q: want open or closed", *model)
	}
	stats.Report(stdout)
	return nil
}

func runAnalyze(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("loadgen analyze", flag.ContinueOnError)
	fs.SetOutput(stdout)
	var c common
	c.register(fs, getenv)
	ops := fs.String("ops", orDefault(getenv("LOADGEN_OPS"), "http://127.0.0.1:8190"), "служебный порт «Заказов» (/metrics, /admin/features)")
	rate := fs.Float64("rate", 150, "сценариев в секунду в шагах 1–3")
	duration := fs.Duration("duration", 15*time.Second, "длительность шага")
	warmup := fs.Duration("warmup", 3*time.Second, "прогрев перед шагом (не входит в статистику)")
	mixSpec := fs.String("mix", "catalog=6,orders=1,history=3", "смесь сценариев")
	overloadRate := fs.Float64("overload-rate", 1500, "шаг 4: заказов в секунду (0 — без шага)")
	overloadDur := fs.Duration("overload-duration", 3*time.Second, "шаг 4: длительность")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	opts, err := c.options(true)
	if err != nil {
		return err
	}
	if *rate <= 0 || *duration <= 0 || *warmup < 0 || *overloadRate < 0 {
		return errors.New("-rate, -duration must be positive; -warmup, -overload-rate non-negative")
	}
	return analyze.Run(ctx, analyze.Config{
		Target: strings.TrimRight(c.target, "/"), Ops: strings.TrimRight(*ops, "/"),
		Rate: *rate, Duration: *duration, Warmup: *warmup, Mix: *mixSpec, Options: opts,
		OverloadRate: *overloadRate, OverloadDuration: *overloadDur,
		Timeout: c.timeout, Seed: c.seed, Out: stdout, ReadyTimeout: 90 * time.Second,
	})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
