package app

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gotutor/labs/gameserver/internal/gateway"
	"gotutor/labs/gameserver/internal/mc"
	"gotutor/labs/gameserver/internal/world"
)

// Config — вся конфигурация сервера.
type Config struct {
	MCAddr   string // Minecraft (TCP)
	WSAddr   string // WebSocket-шлюз (HTTP)
	OpsAddr  string // служебный HTTP: /metrics, /healthz, /readyz, /debug/pprof
	SavePath string // файл снапшота мира ("" — без сохранения)
	SaveEach time.Duration
	LogLevel slog.Level
	Pprof    bool

	World   world.Config
	MC      mc.Config
	Gateway gateway.Config
}

// LoadConfig читает переменные окружения GAME_*.
func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{
		MCAddr: ":25565", WSAddr: ":25580", OpsAddr: ":25590",
		SaveEach: 30 * time.Second, LogLevel: slog.LevelInfo, Pprof: true,
		World: world.DefaultConfig(), MC: mc.DefaultConfig(), Gateway: gateway.DefaultConfig(),
	}
	var errs []string
	str := func(name string, dst *string) {
		if v := getenv(name); v != "" {
			*dst = v
		}
	}
	num := func(name string, dst *int, lo, hi int) {
		v := getenv(name)
		if v == "" {
			return
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < lo || n > hi {
			errs = append(errs, fmt.Sprintf("%s=%q: want integer in [%d, %d]", name, v, lo, hi))
			return
		}
		*dst = n
	}
	flt := func(name string, dst *float64, lo, hi float64) {
		v := getenv(name)
		if v == "" {
			return
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < lo || f > hi {
			errs = append(errs, fmt.Sprintf("%s=%q: want number in [%g, %g]", name, v, lo, hi))
			return
		}
		*dst = f
	}
	dur := func(name string, dst *time.Duration) {
		v := getenv(name)
		if v == "" {
			return
		}
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Sprintf("%s=%q: want positive duration", name, v))
			return
		}
		*dst = d
	}

	str("GAME_MC_ADDR", &c.MCAddr)
	str("GAME_WS_ADDR", &c.WSAddr)
	str("GAME_OPS_ADDR", &c.OpsAddr)
	str("GAME_SAVE_PATH", &c.SavePath)
	dur("GAME_SAVE_INTERVAL", &c.SaveEach)
	str("GAME_MOTD", &c.MC.MOTD)
	num("GAME_MAX_PLAYERS", &c.World.MaxPlayers, 1, 10000)
	num("GAME_MAX_CONNS", &c.MC.MaxConns, 1, 100000)
	num("GAME_WS_MAX_CONNS", &c.Gateway.MaxConns, 1, 100000)
	num("GAME_VIEW_DISTANCE", &c.World.ViewDistance, 2, 16)
	num("GAME_CHUNKS_PER_TICK", &c.World.ChunksPerTick, 1, 1024)
	num("GAME_COMPRESSION_THRESHOLD", &c.MC.CompressionThreshold, -1, 1<<20)
	num("GAME_QUEUE_LEN", &c.MC.QueueLen, 1, 4096)
	num("GAME_MAX_BLOCK_EDITS", &c.World.MaxEdits, 0, 10_000_000)
	flt("GAME_BORDER_RADIUS", &c.World.BorderRadius, 32, 100000)
	flt("GAME_MAX_SPEED", &c.World.MaxSpeed, 1, 1000)
	if v := getenv("GAME_WS_ORIGINS"); v != "" {
		c.Gateway.Origins = nil
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				c.Gateway.Origins = append(c.Gateway.Origins, o)
			}
		}
	}
	if v := getenv("GAME_LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Sprintf("GAME_LOG_LEVEL=%q: %v", v, err))
		}
	}
	if v := getenv("GAME_PPROF"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("GAME_PPROF=%q: want true/false", v))
		}
		c.Pprof = b
	}
	c.Gateway.QueueLen = c.MC.QueueLen
	c.World.SaveEveryTicks = 0
	if c.SavePath != "" {
		c.World.SaveEveryTicks = max(1, int(c.SaveEach/world.TickInterval))
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return c, nil
}
