// Сценарий «Под капотом»: три горутины, мьютекс и WaitGroup.
import { arrow, defineScenario, goroutine, label, memory, scene, struct } from "../../../viz/src/index.ts";
import type { GoroutineState } from "../../../viz/src/index.ts";

const code = `var mu sync.Mutex
var wg sync.WaitGroup
count := 0
for range 3 {
	wg.Add(1)
	go func() {
		defer wg.Done()
		mu.Lock()
		count++
		mu.Unlock()
	}()
}
wg.Wait()
fmt.Println(count)`;

type Name = "main" | "G1" | "G2" | "G3";
type G = [GoroutineState, string] | undefined;

interface Frame {
  g: Partial<Record<Name, G>>;
  /** Кто держит мьютекс. */
  holder?: Name;
  /** Очередь ждущих на семафоре мьютекса. */
  queue: Name[];
  counter: number;
  waiters: number;
  count: number;
  hlMutex?: string[];
  hlWg?: string[];
  hlCount?: boolean;
  hlG?: Name[];
}

const G_X = 10;
const G_W = 250;
const gY: Record<Name, number> = { main: 30, G1: 95, G2: 145, G3: 195 };
const MU = { x: 330, y: 20 };
const QUEUE = { x: 330, y: 168 };
const WG = { x: 330, y: 232 };
const COUNT = { x: 550, y: 50 };

function frame(f: Frame) {
  const parts: Parameters<typeof scene> = [];
  for (const name of ["main", "G1", "G2", "G3"] as Name[]) {
    const g = f.g[name];
    if (!g) continue;
    parts.push(goroutine(name, { x: G_X, y: gY[name], w: G_W, name, state: g[0], detail: g[1], hl: f.hlG?.includes(name) }));
  }
  const mu = struct("mu", {
    x: MU.x,
    y: MU.y,
    w: 180,
    title: "mu sync.Mutex",
    fields: [
      ["locked", f.holder ? 1 : 0],
      ["starving", 0],
      ["waiters", f.queue.length],
    ],
    hl: f.hlMutex,
  });
  const queue = memory("queue", {
    x: QUEUE.x,
    y: QUEUE.y,
    values: [0, 1].map((i) => f.queue[i] ?? null),
    title: "очередь ждущих Lock",
    cellW: 60,
    cellH: 30,
    indexes: false,
  });
  const wg = struct("wg", {
    x: WG.x,
    y: WG.y,
    w: 180,
    title: "wg sync.WaitGroup",
    fields: [
      ["counter", f.counter],
      ["waiters", f.waiters],
    ],
    hl: f.hlWg,
  });
  const count = memory("count", { x: COUNT.x, y: COUNT.y, values: [f.count], title: "count", cellW: 60, indexes: false, hl: f.hlCount ? [0] : [] });
  parts.push(mu, queue, wg, count);

  // Владелец мьютекса: стрелка к строке locked.
  if (f.holder) {
    const from = { x: G_X + G_W, y: gY[f.holder] + 17 };
    parts.push(arrow("hold", from, { x: MU.x, y: MU.y + 39 }, { enter: "left" }));
  }
  // main ждёт в wg.Wait: стрелка к строке waiters.
  if (f.waiters > 0) {
    parts.push(arrow("wait", { x: G_X + G_W, y: gY.main + 17 }, { x: WG.x, y: WG.y + 65 }, { enter: "left" }));
  }
  return scene(...parts);
}

const runnable: G = ["runnable", "готова к запуску"];

export default defineScenario({
  title: "Mutex и WaitGroup",
  code,
  width: 640,
  height: 350,
  steps: [
    {
      lines: [1, 2, 3],
      note: "Нулевые значения `sync.Mutex` и `sync.WaitGroup` готовы к работе: замок открыт, счётчик задач равен нулю. Внутри мьютекса — слово состояния (флаги и число ждущих) и семафор, на котором засыпают горутины.",
      scene: frame({ g: { main: ["running", "выполняется"] }, queue: [], counter: 0, waiters: 0, count: 0 }),
    },
    {
      lines: [4, 5, 6],
      note: "Цикл трижды делает `wg.Add(1)` и запускает горутину. `Add` стоит до `go`, поэтому счётчик дошёл до 3 раньше, чем `main` позовёт `Wait`. Горутины пока только готовы к запуску.",
      scene: frame({ g: { main: ["running", "выполняется"], G1: runnable, G2: runnable, G3: runnable }, queue: [], counter: 3, waiters: 0, count: 0, hlWg: ["counter"] }),
    },
    {
      lines: [13],
      note: "`wg.Wait()` видит счётчик 3 > 0: `main` записывается в ждущие WaitGroup и засыпает на её семафоре. Процессор достаётся горутинам.",
      scene: frame({ g: { main: ["blocked", "ждёт в wg.Wait"], G1: runnable, G2: runnable, G3: runnable }, queue: [], counter: 3, waiters: 1, count: 0, hlWg: ["waiters"] }),
    },
    {
      lines: [8],
      note: "G1 вызывает `mu.Lock()`. Замок открыт — быстрый путь: одна атомарная операция CompareAndSwap ставит флаг `locked`, без участия планировщика.",
      scene: frame({ g: { main: ["blocked", "ждёт в wg.Wait"], G1: ["running", "держит mu"], G2: runnable, G3: runnable }, holder: "G1", queue: [], counter: 3, waiters: 1, count: 0, hlMutex: ["locked"], hlG: ["G1"] }),
    },
    {
      lines: [8],
      note: "G2 и G3 тоже зовут `Lock`, но замок занят. Каждая немного крутится в цикле (spinning) в надежде, что G1 скоро отпустит, затем встаёт в очередь семафора и засыпает. Ждущие не тратят процессор.",
      scene: frame({ g: { main: ["blocked", "ждёт в wg.Wait"], G1: ["running", "держит mu"], G2: ["blocked", "ждёт в mu.Lock"], G3: ["blocked", "ждёт в mu.Lock"] }, holder: "G1", queue: ["G2", "G3"], counter: 3, waiters: 1, count: 0, hlMutex: ["waiters"], hlG: ["G2", "G3"] }),
    },
    {
      lines: [9, 10],
      note: "G1 увеличивает `count` и вызывает `Unlock`: флаг `locked` снят, первая в очереди G2 разбужена. В нормальном режиме разбуженная горутина не получает замок автоматически, а соревнуется за него с новыми — те уже на процессоре и часто выигрывают.",
      scene: frame({ g: { main: ["blocked", "ждёт в wg.Wait"], G1: ["running", "Unlock"], G2: ["runnable", "разбужена, ждёт замок"], G3: ["blocked", "ждёт в mu.Lock"] }, queue: ["G3"], counter: 3, waiters: 1, count: 1, hlCount: true, hlMutex: ["locked", "waiters"], hlG: ["G2"] }),
    },
    {
      lines: [7],
      note: "Отложенный `wg.Done()` уменьшает счётчик до 2, G1 завершается. G2 захватывает замок. Если бы разбуженная горутина безуспешно ждала дольше 1 мс, мьютекс перешёл бы в режим голодания (`starving`): тогда `Unlock` передаёт замок первой в очереди напрямую, а новые горутины сразу встают в хвост.",
      scene: frame({ g: { main: ["blocked", "ждёт в wg.Wait"], G1: ["done", "завершилась"], G2: ["running", "держит mu"], G3: ["blocked", "ждёт в mu.Lock"] }, holder: "G2", queue: ["G3"], counter: 2, waiters: 1, count: 1, hlWg: ["counter"], hlMutex: ["locked"], hlG: ["G2"] }),
    },
    {
      lines: [9, 10, 7],
      note: "G2 делает то же: `count` становится 2, `Unlock` будит G3, `Done` уменьшает счётчик до 1. Теперь замок у G3, очередь пуста.",
      scene: frame({ g: { main: ["blocked", "ждёт в wg.Wait"], G1: ["done", "завершилась"], G2: ["done", "завершилась"], G3: ["running", "держит mu"] }, holder: "G3", queue: [], counter: 1, waiters: 1, count: 2, hlCount: true, hlWg: ["counter"], hlG: ["G3"] }),
    },
    {
      lines: [9, 10, 7],
      note: "G3 увеличивает `count` до 3 и отпускает замок — будить некого. Её `Done` обнуляет счётчик, и WaitGroup будит всех, кто ждёт в `Wait`: `main` снова готова к запуску.",
      scene: frame({ g: { main: ["runnable", "разбужена: counter = 0"], G1: ["done", "завершилась"], G2: ["done", "завершилась"], G3: ["done", "завершилась"] }, queue: [], counter: 0, waiters: 0, count: 3, hlCount: true, hlWg: ["counter", "waiters"], hlG: ["main"] }),
    },
    {
      lines: [14],
      note: "`main` печатает 3. Чтение `count` без мьютекса здесь безопасно: каждый `Unlock` «происходит до» следующего `Lock`, а каждый `Done` — до возврата из `Wait`, поэтому все три записи видны.",
      scene: scene(
        frame({ g: { main: ["running", "fmt.Println(count) → 3"], G1: ["done", "завершилась"], G2: ["done", "завершилась"], G3: ["done", "завершилась"] }, queue: [], counter: 0, waiters: 0, count: 3 }),
        label("legend", G_X, 338, "Порядок G1 → G2 → G3 — один из возможных: кто возьмёт замок первым, решает планировщик.", "v-index"),
      ),
    },
  ],
});
