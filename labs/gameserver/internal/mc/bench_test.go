package mc

import (
	"context"
	"fmt"
	"testing"

	"gotutor/labs/gameserver/internal/world"
)

// BenchmarkTick200 — тик мира с 200 Minecraft-игроками, которые все
// двигаются (худший случай: все в одной зоне видимости, каждое
// перемещение уходит 199 зрителям). Показывает MSPT без сети и
// аллокации на тик:
//
//	go test -run=^$ -bench=Tick200 -benchmem ./internal/mc
func BenchmarkTick200(b *testing.B) {
	cfg := world.DefaultConfig()
	cfg.ChunksPerTick = 1000
	cfg.MaxSpeed = 1000
	w := world.New(cfg, world.Options{}, nil)
	enc := NewEncoder(256)
	ctx := context.Background()
	const n = 200
	ids := make([]world.EntityID, n)
	peers := make([]*world.Peer, n)
	for i := range n {
		peers[i] = world.NewPeer(world.KindMinecraft, enc, 1024)
		// Join ждёт ответа мира, а мир отвечает в Step — поэтому Join в
		// горутине, а id забираем через канал (без гонки за ids).
		res := make(chan world.EntityID, 1)
		go func() {
			id, err := w.Join(ctx, fmt.Sprintf("p%03d", i), [16]byte{byte(i)}, peers[i])
			if err != nil {
				panic(err)
			}
			res <- id
		}()
		var id world.EntityID
		for id == 0 {
			w.Step()
			select {
			case id = <-res:
			default:
			}
		}
		ids[i] = id
	}
	drain := func() {
		for _, p := range peers {
			for len(p.Out()) > 0 {
				world.PutBuffer(<-p.Out())
			}
		}
	}
	for _, id := range ids {
		w.Submit(ctx, world.Command{Kind: world.CmdTeleportAck, Player: id, TeleportID: 1})
	}
	w.Step()
	drain()
	b.ReportAllocs()
	b.ResetTimer()
	for t := 0; b.Loop(); t++ {
		b.StopTimer()
		for i, id := range ids {
			// Все стоят сеткой 20×10 у точки появления и шагают по x
			// туда-обратно: 0…7.8 блока шагом 0.2, затем скачок назад.
			x := float64(i%20) + float64(t%40)*0.2
			w.Submit(ctx, world.Command{Kind: world.CmdMove, Player: id, HasPos: true, HasRot: true,
				Pos: world.Vec3{X: x, Y: world.SpawnY, Z: float64(i / 20)}, Yaw: float32(t % 360)})
		}
		b.StartTimer()
		w.Step()
		b.StopTimer()
		drain()
		b.StartTimer()
	}
}
