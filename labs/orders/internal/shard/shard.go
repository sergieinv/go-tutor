// Package shard — шардирование заказов по покупателю (модуль 14).
//
// Ключ шардирования — customer_id: все заказы покупателя живут на одном
// шарде, поэтому история (самый частый запрос) читается из одной базы, а
// транзакция заказа (заказ + позиции + outbox) не выходит за пределы
// шарда. Распределённых транзакций нет.
//
// Шард покупателя выбирает jump consistent hash (Lamping, Veach, 2014):
//
//   - стабильная функция без состояния: ни таблицы соответствий, ни
//     кольца с виртуальными узлами — только число шардов;
//   - равномерная, O(ln n) по времени и O(1) по памяти;
//   - при добавлении шарда N+1 переезжает минимум — 1/(N+1) покупателей, и
//     только на новый шард.
//
// Её ограничение — шарды нумеруются 0..N-1 и добавляются только в конец
// (убрать шард из середины нельзя). Здесь это не мешает: номер шарда и так
// зашит в номер заказа и меняться не может. Rendezvous hashing (HRW)
// позволил бы удалять произвольный шард, но стоит O(n) на каждый выбор и
// требует стабильных имён шардов — здесь не нужно.
//
// Номер шарда — в старших битах номера заказа: id = shard<<48 | seq, где
// seq — последовательность в базе шарда. GetOrder и CancelOrder находят
// шард по номеру без опроса всех. Совместимость: у шарда 0 номера те же,
// что без шардирования (1, 2, 3…), поэтому включение шардов не меняет уже
// выданные номера, а база модулей 10–12 становится шардом 0 как есть.
// 48 бит на последовательность — 2.8·10^14 заказов на шард; 2^48 меньше
// 2^53, номер шарда 1 (281474976710657) ещё точно представим и в
// JavaScript, хотя int64 в protojson всё равно идёт строкой.
//
// Чего нет (честно): решардирования. Добавили шард — jump hash отправит
// новые заказы части покупателей на него, а их старые заказы останутся на
// прежнем шарде: GetOrder их найдёт (шард в номере), а история покажет
// только новые. Перед добавлением шарда переезжающих покупателей переносят
// (копия → переключение → удаление), и это отдельный инструмент.
package shard

import (
	"context"
	"fmt"

	"gotutor/labs/orders/internal/orders"
)

// Bits — сколько младших бит номера заказа занимает последовательность
// шарда.
const Bits = 48

// MaxShards — сколько шардов помещается в оставшиеся биты int64.
const MaxShards = 1 << (63 - Bits)

// Jump — jump consistent hash: номер корзины [0, buckets) для ключа.
// Код — из статьи «A Fast, Minimal Memory, Consistent Hash Algorithm».
func Jump(key uint64, buckets int) int {
	var b, j int64 = -1, 0
	for j < int64(buckets) {
		b = j
		key = key*2862933555777941757 + 1
		j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((key>>33)+1)))
	}
	return int(b)
}

// mix — финализатор splitmix64: перемешивает биты customer_id перед jump
// hash. Номера покупателей идут подряд, а линейный генератор внутри Jump
// для близких ключей на первых шагах даёт близкие значения.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// OfCustomer — шард покупателя при n шардах.
func OfCustomer(customerID int64, n int) int {
	if n <= 1 {
		return 0
	}
	return Jump(mix(uint64(customerID)), n)
}

// OfOrder — шард заказа по его номеру.
func OfOrder(orderID int64) int { return int(orderID >> Bits) }

// Router — orders.Router над набором шардов.
type Router struct {
	shards []orders.Store
}

var _ orders.Router = (*Router)(nil)

// New создаёт роутер. shards[0] — прежняя (единственная) база.
func New(shards []orders.Store) (*Router, error) {
	if len(shards) == 0 || len(shards) > MaxShards {
		return nil, fmt.Errorf("shard: want 1..%d shards, got %d", MaxShards, len(shards))
	}
	r := &Router{shards: make([]orders.Store, len(shards))}
	for i, s := range shards {
		if i == 0 {
			r.shards[i] = s // у шарда 0 номера заказов не меняются
			continue
		}
		r.shards[i] = &numbered{Store: s, shard: int64(i)}
	}
	return r, nil
}

// Len — число шардов.
func (r *Router) Len() int { return len(r.shards) }

// ShardOf — номер шарда покупателя.
func (r *Router) ShardOf(customerID int64) int { return OfCustomer(customerID, len(r.shards)) }

// ForCustomer — хранилище шарда покупателя.
func (r *Router) ForCustomer(customerID int64) orders.Store {
	return r.shards[r.ShardOf(customerID)]
}

// ForOrder — хранилище шарда заказа. Номер с несуществующим шардом —
// такого заказа нет.
func (r *Router) ForOrder(orderID int64) (orders.Store, error) {
	i := OfOrder(orderID)
	if orderID <= 0 || i >= len(r.shards) {
		return nil, fmt.Errorf("order %d: unknown shard %d: %w", orderID, i, orders.ErrNotFound)
	}
	return r.shards[i], nil
}

// numbered дописывает номер шарда в номера заказов.
type numbered struct {
	orders.Store
	shard int64
}

func (n *numbered) NextOrderID(ctx context.Context) (int64, error) {
	seq, err := n.Store.NextOrderID(ctx)
	if err != nil {
		return 0, err
	}
	if seq <= 0 || seq >= 1<<Bits {
		return 0, fmt.Errorf("shard %d: order sequence %d out of range", n.shard, seq)
	}
	return n.shard<<Bits | seq, nil
}
