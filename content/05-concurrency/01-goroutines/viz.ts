// Сценарий «Под капотом»: планировщик GMP — очереди P, work stealing,
// handoff P при системном вызове и асинхронное вытеснение.
import { box, defineScenario, label, memory, scene } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `runtime.GOMAXPROCS(2)
var wg sync.WaitGroup
for i := range 4 {
	wg.Add(1)
	go func() { // G1…G4 для i = 0…3
		defer wg.Done()
		work(i) // G2 — долгий цикл, G4 — syscall
	}()
}
wg.Wait() // main засыпает до конца работы`;

// Геометрия: два P в колонках, глобальная очередь сверху,
// системный вызов и sysmon снизу, заблокированные горутины — в самом низу.
const COL = [10, 330];
const P_Y = 130;
const SLOT_Y = 178;
const SLOT_W = 52;
const RUN_DX = 10;
const NEXT_DX = 80;
const Q_DX = 140;
const Q_CAP = 3;
const GQ = { x: 10, y: 44, cap: 4 };
const SYS = { x: 10, y: 265 };
const BLOCKED = { x: 10, y: 375 };

interface PState {
  /** Поток, к которому привязан P, и что он делает. */
  m?: string;
  run?: string;
  next?: string;
  q: string[];
  status: string;
}

interface Frame {
  gq: string[];
  p: [PState, PState];
  /** Поток в системном вызове и его горутина. */
  sys?: { m: string; g: string };
  sysmon: string[];
  blocked: string[];
  hl?: string[];
}

// Горутина — фишка со стабильным id: при переходе между очередями
// плеер плавно её переносит.
function chip(name: string, x: number, y: number, hl: boolean): Shape[] {
  return [
    { type: "rect", id: `g:${name}:chip`, x, y, w: SLOT_W - 4, h: 28, rx: 6, cls: hl ? "v-cell v-hl" : "v-cell" },
    { type: "text", id: `g:${name}:name`, x: x + (SLOT_W - 4) / 2, y: y + 19, text: name, anchor: "middle", cls: "v-value" },
  ];
}

const inSlot = (x: number, y: number) => ({ x: x + 2, y: y + 4 });

function frame(f: Frame) {
  const hl = new Set(f.hl ?? []);
  const parts: Parameters<typeof scene> = [];
  const chips: Shape[] = [];
  const put = (g: string, at: { x: number; y: number }) => chips.push(...chip(g, at.x, at.y, hl.has(g)));

  parts.push(memory("gq", { x: GQ.x, y: GQ.y, values: Array(GQ.cap).fill(null), title: "глобальная очередь (общая для всех P)", cellW: SLOT_W, indexes: false }));
  f.gq.forEach((g, i) => put(g, inSlot(GQ.x + i * SLOT_W, GQ.y)));
  parts.push(label("legend", 330, 64, "M — поток ОС, P — право выполнять Go-код", "v-caption"));

  f.p.forEach((p, i) => {
    const x0 = COL[i];
    if (p.m) parts.push(box(`m:${p.m.split(" ")[0]}`, { x: x0 + 10, y: 88, w: 170, h: 30, title: p.m }));
    parts.push(box(`p${i}`, { x: x0, y: P_Y, w: 300, h: 115, title: `P${i}` }));
    parts.push(memory(`p${i}:run`, { x: x0 + RUN_DX, y: SLOT_Y, values: [null], title: "выполняет", cellW: SLOT_W, indexes: false }));
    parts.push(memory(`p${i}:next`, { x: x0 + NEXT_DX, y: SLOT_Y, values: [null], title: "runnext", cellW: SLOT_W, indexes: false }));
    parts.push(memory(`p${i}:q`, { x: x0 + Q_DX, y: SLOT_Y, values: Array(Q_CAP).fill(null), title: "локальная очередь", cellW: SLOT_W, indexes: false }));
    parts.push(label(`p${i}:status`, x0 + 10, 233, p.status, "v-caption"));
    if (p.run) put(p.run, inSlot(x0 + RUN_DX, SLOT_Y));
    if (p.next) put(p.next, inSlot(x0 + NEXT_DX, SLOT_Y));
    p.q.forEach((g, j) => put(g, inSlot(x0 + Q_DX + j * SLOT_W, SLOT_Y)));
  });

  parts.push(box("sys", { x: SYS.x, y: SYS.y, w: 300, h: 95, title: "системный вызов (в ядре ОС)", cls: f.sys ? "v-waiting" : undefined }));
  if (f.sys) {
    parts.push(box(`m:${f.sys.m}`, { x: SYS.x + 10, y: SYS.y + 35, w: 170, h: 30, title: `${f.sys.m} · ждёт ядро` }));
    put(f.sys.g, { x: SYS.x + 200, y: SYS.y + 36 });
  }
  parts.push(box("sysmon", { x: 330, y: SYS.y, w: 300, h: 95, title: "sysmon — поток без P", lines: f.sysmon }));

  parts.push(box("blocked", { x: BLOCKED.x, y: BLOCKED.y, w: 620, h: 44, title: "заблокированы — не в очередях" }));
  f.blocked.forEach((g, i) => put(g, { x: 330 + i * SLOT_W, y: BLOCKED.y + 8 }));

  return scene(...parts, chips);
}

const idle: PState = { q: [], status: "простаивает: нет ни M, ни работы" };
const quiet = ["проверяет P раз в 20 мкс … 10 мс"];

export default defineScenario({
  title: "Планировщик GMP: очереди, кража работы, syscall и вытеснение",
  code,
  width: 640,
  height: 430,
  steps: [
    {
      lines: [1],
      note: "`GOMAXPROCS(2)` — два P. P — это право выполнять Go-код со своей очередью горутин. Поток ОС (M) выполняет горутины (G), только пока держит P. Сейчас M0 с P0 выполняет `main`, P1 простаивает.",
      scene: frame({
        gq: [],
        p: [{ m: "M0 · поток ОС", run: "main", q: [], status: "M0 выполняет main" }, idle],
        sysmon: quiet,
        blocked: [],
        hl: ["main"],
      }),
    },
    {
      lines: [5],
      note: "`go` создаёт G1 и кладёт её не в общую очередь, а в слот `runnext` своего P0 — она выполнится следующей. `main` не ждёт и идёт дальше. Раз есть свободный P1, рантайм будит поток M1 искать работу.",
      scene: frame({
        gq: [],
        p: [
          { m: "M0 · поток ОС", run: "main", next: "G1", q: [], status: "M0 выполняет main" },
          { m: "M1 · ищет работу", q: [], status: "M1 просыпается" },
        ],
        sysmon: quiet,
        blocked: [],
        hl: ["G1"],
      }),
    },
    {
      lines: [5],
      note: "Следующий `go`: G2 занимает `runnext`, а G1 вытесняется в хвост локальной очереди P0. Свежая горутина обычно продолжает работу создателя — её данные ещё в кеше процессора.",
      scene: frame({
        gq: [],
        p: [
          { m: "M0 · поток ОС", run: "main", next: "G2", q: ["G1"], status: "M0 выполняет main" },
          { m: "M1 · ищет работу", q: [], status: "M1 просыпается" },
        ],
        sysmon: quiet,
        blocked: [],
        hl: ["G2", "G1"],
      }),
    },
    {
      lines: [3, 4, 5, 6, 7, 8, 9],
      note: "Ещё две итерации: в `runnext` теперь G4, в локальной очереди — G1, G2, G3. Локальная очередь P вмещает 256 горутин; при переполнении половина уходит в глобальную очередь.",
      scene: frame({
        gq: [],
        p: [
          { m: "M0 · поток ОС", run: "main", next: "G4", q: ["G1", "G2", "G3"], status: "M0 выполняет main" },
          { m: "M1 · ищет работу", q: [], status: "M1 ищет: своя очередь и глобальная пусты" },
        ],
        sysmon: quiet,
        blocked: [],
        hl: ["G4", "G3"],
      }),
    },
    {
      lines: [5],
      note: "Work stealing: у M1 своя очередь пуста, глобальная тоже — он крадёт у P0 половину очереди (3 − 3/2 = 2 горутины, с головы). Последнюю украденную, G2, P1 запускает сразу, G1 кладёт к себе в очередь.",
      scene: frame({
        gq: [],
        p: [
          { m: "M0 · поток ОС", run: "main", next: "G4", q: ["G3"], status: "M0 выполняет main" },
          { m: "M1 · поток ОС", run: "G2", q: ["G1"], status: "M1 выполняет G2" },
        ],
        sysmon: quiet,
        blocked: [],
        hl: ["G1", "G2"],
      }),
    },
    {
      lines: [10],
      note: "`wg.Wait()` блокирует `main`. Заблокированная горутина не стоит ни в одной очереди и не занимает поток: M0 сразу берёт следующую — сначала из `runnext`, это G4.",
      scene: frame({
        gq: [],
        p: [
          { m: "M0 · поток ОС", run: "G4", q: ["G3"], status: "M0 выполняет G4" },
          { m: "M1 · поток ОС", run: "G2", q: ["G1"], status: "M1 выполняет G2" },
        ],
        sysmon: quiet,
        blocked: ["main"],
        hl: ["main", "G4"],
      }),
    },
    {
      lines: [7],
      note: "G4 делает блокирующий системный вызов (например, читает файл). Поток M0 уходит в ядро вместе с G4 и не может выполнять Go-код. P0 пока числится за ним, и G3 в его очереди простаивает.",
      scene: frame({
        gq: [],
        p: [
          { q: ["G3"], status: "P0 закреплён за M0, который в syscall" },
          { m: "M1 · поток ОС", run: "G2", q: ["G1"], status: "M1 выполняет G2" },
        ],
        sys: { m: "M0", g: "G4" },
        sysmon: quiet,
        blocked: ["main"],
        hl: ["G4"],
      }),
    },
    {
      lines: [7],
      note: "Handoff: sysmon замечает, что P0 застрял в syscall дольше одного своего тика, а в очереди есть работа. Он отбирает P0 у M0 и отдаёт другому потоку — M2 (спящему или новому). M2 запускает G3. Поэтому потоков ОС бывает больше, чем GOMAXPROCS.",
      scene: frame({
        gq: [],
        p: [
          { m: "M2 · поток ОС", run: "G3", q: [], status: "M2 выполняет G3" },
          { m: "M1 · поток ОС", run: "G2", q: ["G1"], status: "M1 выполняет G2" },
        ],
        sys: { m: "M0", g: "G4" },
        sysmon: ["P0 в syscall и есть работа:", "отдаю P0 потоку M2 (handoffp)"],
        blocked: ["main"],
        hl: ["G3"],
      }),
    },
    {
      lines: [7],
      note: "Вытеснение: G2 крутит цикл без вызовов функций уже больше 10 мс. sysmon шлёт потоку M1 сигнал (SIGURG), обработчик останавливает G2 в безопасной точке и кладёт в глобальную очередь. P1 берёт G1 из своей очереди.",
      scene: frame({
        gq: ["G2"],
        p: [
          { m: "M2 · поток ОС", run: "G3", q: [], status: "M2 выполняет G3" },
          { m: "M1 · поток ОС", run: "G1", q: [], status: "M1 выполняет G1" },
        ],
        sys: { m: "M0", g: "G4" },
        sysmon: ["G2 занимает P1 больше 10 мс:", "вытесняю сигналом (preemptone)"],
        blocked: ["main"],
        hl: ["G2", "G1"],
      }),
    },
    {
      lines: [7],
      note: "Системный вызов G4 вернулся, но свободного P нет — оба заняты. G4 становится в глобальную очередь, а поток M0 засыпает в списке простаивающих: без P ему нечего делать.",
      scene: frame({
        gq: ["G2", "G4"],
        p: [
          { m: "M2 · поток ОС", run: "G3", q: [], status: "M2 выполняет G3" },
          { m: "M1 · поток ОС", run: "G1", q: [], status: "M1 выполняет G1" },
        ],
        sysmon: quiet,
        blocked: ["main"],
        hl: ["G4"],
      }),
    },
    {
      lines: [6, 7],
      note: "G3 закончила. Своя очередь P0 пуста — M2 идёт в глобальную и забирает пачку: G2 запускает, G4 кладёт в локальную очередь. Глобальную очередь P проверяет и при непустой своей — раз в 61 запуск, чтобы она не голодала.",
      scene: frame({
        gq: [],
        p: [
          { m: "M2 · поток ОС", run: "G2", q: ["G4"], status: "M2 выполняет G2" },
          { m: "M1 · поток ОС", run: "G1", q: [], status: "M1 выполняет G1" },
        ],
        sysmon: quiet,
        blocked: ["main"],
        hl: ["G2", "G4"],
      }),
    },
    {
      lines: [10],
      note: "Последний `wg.Done()` будит `main`: он попадает в очередь того P, где выполнился `Done`, и `wg.Wait()` возвращается. Весь путь горутин — очереди P, кража, handoff и вытеснение — скрыт от кода: он остаётся обычным синхронным.",
      scene: frame({
        gq: [],
        p: [
          { m: "M2 · поток ОС", run: "main", q: [], status: "M2 выполняет main" },
          { m: "M1 · ищет работу", q: [], status: "работы нет — M1 скоро заснёт" },
        ],
        sysmon: quiet,
        blocked: [],
        hl: ["main"],
      }),
    },
  ],
});
