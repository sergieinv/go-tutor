package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"gotutor/labs/gameserver/internal/websocket"
)

// wsBot — браузерный игрок по протоколу WebSocket: hello, подтверждение
// телепортов, ходьба, чат. Разбирает JSON только у нужных сообщений.
type wsBot struct {
	name  string
	cfg   *botConfig
	stats *stats

	conn *websocket.Conn
	mu   sync.Mutex
	pos  [3]float64
	have bool
}

func (b *wsBot) run(ctx context.Context) error {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := websocket.Dial(dctx, "ws://"+b.cfg.wsAddr+"/ws", "")
	cancel()
	if err != nil {
		return joinError{err}
	}
	b.conn = conn
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := conn.WriteMessage(websocket.OpText, []byte(`{"t":"hello","name":"`+b.name+`"}`)); err != nil {
		return joinError{err}
	}
	walkerDone := make(chan struct{})
	go func() {
		defer close(walkerDone)
		b.walk(ctx)
	}()
	err = b.readLoop()
	conn.Close()
	<-walkerDone
	return err
}

var (
	markWelcome  = []byte(`"t":"welcome"`)
	markTeleport = []byte(`"t":"teleport"`)
	markKick     = []byte(`"t":"kick"`)
	markChat     = []byte(`"t":"chat"`)
)

func (b *wsBot) readLoop() error {
	for {
		_, data, err := b.conn.ReadMessage()
		if err != nil {
			return err
		}
		b.stats.packetsIn.Add(1)
		b.stats.chatIn.Add(int64(bytes.Count(data, markChat)))
		if !bytes.Contains(data, markWelcome) && !bytes.Contains(data, markTeleport) && !bytes.Contains(data, markKick) {
			continue
		}
		var batch []struct {
			T      string  `json:"t"`
			X      float64 `json:"x"`
			Y      float64 `json:"y"`
			Z      float64 `json:"z"`
			TP     int32   `json:"tp"`
			Reason string  `json:"reason"`
		}
		if err := json.Unmarshal(data, &batch); err != nil {
			return err
		}
		for _, ev := range batch {
			switch ev.T {
			case "welcome", "teleport":
				b.mu.Lock()
				if ev.T == "teleport" {
					b.stats.corrections.Add(1)
				} else {
					b.stats.joined.Add(1)
				}
				b.pos, b.have = [3]float64{ev.X, ev.Y, ev.Z}, true
				b.mu.Unlock()
				if err := b.conn.WriteMessage(websocket.OpText, fmt.Appendf(nil, `{"t":"tpack","tp":%d}`, ev.TP)); err != nil {
					return err
				}
			case "kick":
				b.mu.Lock()
				joined := b.have
				b.mu.Unlock()
				if !joined {
					return joinError{fmt.Errorf("refused: %s", ev.Reason)}
				}
				return fmt.Errorf("kicked: %s", ev.Reason)
			}
		}
	}
}

func (b *wsBot) walk(ctx context.Context) {
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7))
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	tx, tz := (rng.Float64()*2-1)*b.cfg.spread, (rng.Float64()*2-1)*b.cfg.spread
	step := b.cfg.speed / 20
	nextChat := time.Now().Add(time.Duration(rng.Int64N(int64(2 * b.cfg.chatEvery))))
	var buf []byte
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			b.mu.Lock()
			if !b.have {
				b.mu.Unlock()
				continue
			}
			dx, dz := tx-b.pos[0], tz-b.pos[2]
			dist := math.Hypot(dx, dz)
			if dist < step {
				tx, tz = (rng.Float64()*2-1)*b.cfg.spread, (rng.Float64()*2-1)*b.cfg.spread
				b.mu.Unlock()
				continue
			}
			b.pos[0] += dx / dist * step
			b.pos[2] += dz / dist * step
			x, z := b.pos[0], b.pos[2]
			b.mu.Unlock()
			buf = fmt.Appendf(buf[:0], `{"t":"move","x":%.3f,"z":%.3f,"yaw":%.1f}`, x, z, math.Atan2(-dx, dz)*180/math.Pi)
			if b.conn.WriteMessage(websocket.OpText, buf) != nil {
				return
			}
			b.stats.moves.Add(1)
			if b.cfg.chatEvery > 0 && now.After(nextChat) {
				nextChat = now.Add(time.Duration(rng.Int64N(int64(2 * b.cfg.chatEvery))))
				if b.conn.WriteMessage(websocket.OpText, []byte(`{"t":"chat","text":"hello from `+b.name+`"}`)) != nil {
					return
				}
				b.stats.chatOut.Add(1)
			}
		}
	}
}
