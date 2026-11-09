// Сценарий «Под капотом»: планировщик GMP глубже, чем в модуле 5 —
// кража половины очереди, netpoller, handoff P при блокирующем syscall,
// асинхронное вытеснение сигналом и возврат из syscall без свободного P.
import { box, defineScenario, label, memory, scene } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `func main() {
	runtime.GOMAXPROCS(2)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go serve(ln)           // G1: ln.Accept() ждёт клиента
	go spin()              // G2: долгий цикл без вызовов функций
	go readFile("big.bin") // G3: os.ReadFile — блокирующий read(2)
	go work(1)             // G4
	go work(2)             // G5
	time.Sleep(time.Second)
}`;

const COL = [10, 330];
const SLOT = 44;
const GQ = { x: 10, y: 30, cap: 5 };
const M_Y = 82;
const P_Y = 116;
const SLOT_Y = 156;
const RUN_DX = 10;
const NEXT_DX = 64;
const Q_DX = 118;
const Q_CAP = 4;
const NET = { x: 330, y: 8 };
const SYS = { x: 10, y: 236 };
const MON = { x: 330, y: 236 };
const IDLE = { x: 10, y: 326 };
const WAIT = { x: 330, y: 326 };

interface PState {
  m?: string;
  run?: string;
  next?: string;
  q: string[];
  status: string;
}

interface Frame {
  gq: string[];
  p: [PState, PState];
  net: string[];
  sys?: { m: string; g: string };
  sysmon: string[];
  idleM: string[];
  wait: string[];
  hl?: string[];
}

// Фишка горутины со стабильным id: плеер переносит её между очередями.
function chip(name: string, x: number, y: number, hl: boolean): Shape[] {
  return [
    { type: "rect", id: `g:${name}:chip`, x, y, w: SLOT - 4, h: 26, rx: 6, cls: hl ? "v-cell v-hl" : "v-cell" },
    { type: "text", id: `g:${name}:name`, x: x + (SLOT - 4) / 2, y: y + 18, text: name, anchor: "middle", cls: "v-value" },
  ];
}

const inSlot = (x: number, y: number) => ({ x: x + 2, y: y + 5 });
const mBox = (name: string, x: number, y: number, title: string) => box(`m:${name}`, { x, y, w: 150, h: 28, title });

function frame(f: Frame) {
  const hl = new Set(f.hl ?? []);
  const parts: Parameters<typeof scene> = [];
  const chips: Shape[] = [];
  const put = (g: string, at: { x: number; y: number }) => chips.push(...chip(g, at.x, at.y, hl.has(g)));

  parts.push(memory("gq", { x: GQ.x, y: GQ.y, values: Array(GQ.cap).fill(null), title: "глобальная очередь", cellW: SLOT, indexes: false }));
  f.gq.forEach((g, i) => put(g, inSlot(GQ.x + i * SLOT, GQ.y)));

  parts.push(box("net", { x: NET.x, y: NET.y, w: 300, h: 62, title: "netpoller: ждут сеть, поток не занят" }));
  f.net.forEach((g, i) => put(g, { x: NET.x + 10 + i * SLOT, y: NET.y + 28 }));

  f.p.forEach((p, i) => {
    const x0 = COL[i];
    if (p.m) parts.push(mBox(p.m, x0 + 10, M_Y, `${p.m} · поток ОС`));
    parts.push(box(`p${i}`, { x: x0, y: P_Y, w: 300, h: 108, title: `P${i}` }));
    parts.push(memory(`p${i}:run`, { x: x0 + RUN_DX, y: SLOT_Y, values: [null], title: "сейчас", cellW: SLOT, indexes: false }));
    parts.push(memory(`p${i}:next`, { x: x0 + NEXT_DX, y: SLOT_Y, values: [null], title: "runnext", cellW: SLOT, indexes: false }));
    parts.push(memory(`p${i}:q`, { x: x0 + Q_DX, y: SLOT_Y, values: Array(Q_CAP).fill(null), title: "локальная очередь", cellW: SLOT, indexes: false }));
    parts.push(label(`p${i}:status`, x0 + 10, 212, p.status, "v-caption"));
    if (p.run) put(p.run, inSlot(x0 + RUN_DX, SLOT_Y));
    if (p.next) put(p.next, inSlot(x0 + NEXT_DX, SLOT_Y));
    p.q.forEach((g, j) => put(g, inSlot(x0 + Q_DX + j * SLOT, SLOT_Y)));
  });

  parts.push(box("sys", { x: SYS.x, y: SYS.y, w: 300, h: 78, title: "блокирующий syscall (в ядре ОС)", cls: f.sys ? "v-waiting" : undefined }));
  if (f.sys) {
    parts.push(mBox(f.sys.m, SYS.x + 10, SYS.y + 34, `${f.sys.m} · ждёт ядро`));
    put(f.sys.g, { x: SYS.x + 200, y: SYS.y + 35 });
  }
  parts.push(box("sysmon", { x: MON.x, y: MON.y, w: 300, h: 78, title: "sysmon — поток без P", lines: f.sysmon }));

  parts.push(box("idle", { x: IDLE.x, y: IDLE.y, w: 300, h: 86, title: "спящие M (нет P — нечего делать)" }));
  f.idleM.forEach((m, i) => parts.push(mBox(m, IDLE.x + 10, IDLE.y + 28 + i * 32, `${m} · спит`)));

  parts.push(box("wait", { x: WAIT.x, y: WAIT.y, w: 300, h: 86, title: "ждут таймер — не в очередях" }));
  f.wait.forEach((g, i) => put(g, { x: WAIT.x + 10 + i * SLOT, y: WAIT.y + 34 }));

  return scene(...parts, chips);
}

const sleeping = ["спит 20 мкс … 10 мс между проверками"];
const idleP: PState = { q: [], status: "простаивает" };

export default defineScenario({
  title: "Планировщик GMP: кража, netpoller, handoff и вытеснение",
  code,
  width: 640,
  height: 420,
  steps: [
    {
      lines: [2],
      note: "Два P — два права выполнять Go-код. Поток M0 держит P0 и выполняет `main`; P1 пока без потока. Отдельно работает `sysmon` — служебный поток без P: он следит за долгими горутинами, системными вызовами и сетью.",
      scene: frame({ gq: [], p: [{ m: "M0", run: "main", q: [], status: "M0 выполняет main" }, idleP], net: [], sysmon: sleeping, idleM: [], wait: [], hl: ["main"] }),
    },
    {
      lines: [3, 4],
      note: "`go serve(ln)` кладёт G1 в слот `runnext` своего P0. Раз есть свободный P и нет ищущих работу потоков, рантайм будит поток: M1 берёт P1 и становится «крутящимся» (spinning) — ищет, что выполнить.",
      scene: frame({ gq: [], p: [{ m: "M0", run: "main", next: "G1", q: [], status: "M0 выполняет main" }, { m: "M1", q: [], status: "M1 ищет работу (spinning)" }], net: [], sysmon: sleeping, idleM: [], wait: [], hl: ["G1"] }),
    },
    {
      lines: [5, 6, 7, 8],
      note: "Каждый следующий `go` занимает `runnext`, а прежняя горутина оттуда уходит в хвост локальной очереди. После G5: в `runnext` G5, в очереди G1…G4. Локальная очередь вмещает 256 горутин, и пока M1 ищет работу, `main` продолжает без пауз.",
      scene: frame({ gq: [], p: [{ m: "M0", run: "main", next: "G5", q: ["G1", "G2", "G3", "G4"], status: "M0 выполняет main" }, { m: "M1", q: [], status: "M1: своя очередь и глобальная пусты" }], net: [], sysmon: sleeping, idleM: [], wait: [], hl: ["G5", "G4"] }),
    },
    {
      lines: [5, 6, 7, 8],
      note: "Кража (`runqsteal`): M1 забирает у P0 половину очереди с округлением вверх — 4 − 4/2 = 2 горутины с головы. Последнюю украденную, G2, P1 запускает сразу, G1 кладёт к себе. Чужой `runnext` трогают только в последнюю очередь — на четвёртом проходе по соседям.",
      scene: frame({ gq: [], p: [{ m: "M0", run: "main", next: "G5", q: ["G3", "G4"], status: "M0 выполняет main" }, { m: "M1", run: "G2", q: ["G1"], status: "M1 выполняет G2 (spin)" }], net: [], sysmon: sleeping, idleM: [], wait: [], hl: ["G1", "G2"] }),
    },
    {
      lines: [9],
      note: "`time.Sleep` паркует `main` на таймере: горутина не стоит ни в одной очереди, поток свободен. P0 берёт следующую — из `runnext`, это G5. G5 доделывает работу и завершается, за ней P0 берёт G3 с головы локальной очереди.",
      scene: frame({ gq: [], p: [{ m: "M0", run: "G3", q: ["G4"], status: "M0 выполняет G3" }, { m: "M1", run: "G2", q: ["G1"], status: "M1 выполняет G2 (spin)" }], net: [], sysmon: sleeping, idleM: [], wait: ["main"], hl: ["main", "G3"] }),
    },
    {
      lines: [6],
      note: "G3 читает обычный файл. Обычные файлы не работают через netpoller, поэтому `read(2)` — настоящий блокирующий системный вызов: M0 уходит в ядро вместе с G3. P0 по-прежнему закреплён за M0, и G4 в его очереди ждёт.",
      scene: frame({ gq: [], p: [{ q: ["G4"], status: "P0 закреплён за M0, M0 в ядре" }, { m: "M1", run: "G2", q: ["G1"], status: "M1 выполняет G2 (spin)" }], net: [], sys: { m: "M0", g: "G3" }, sysmon: sleeping, idleM: [], wait: ["main"], hl: ["G3"] }),
    },
    {
      lines: [6, 7],
      note: "Handoff (`retake` → `handoffp`): sysmon видит, что вызов длится дольше одного его тика (от 20 мкс), а у P0 есть работа (отобрал бы и без неё, если бы не было ни ищущих работу M, ни свободных P). Он отбирает P0 у M0 и отдаёт другому потоку — спящих нет, поэтому рантайм создаёт M2. M2 запускает G4. Отсюда потоков ОС больше, чем GOMAXPROCS.",
      scene: frame({ gq: [], p: [{ m: "M2", run: "G4", q: [], status: "M2 выполняет G4" }, { m: "M1", run: "G2", q: ["G1"], status: "M1 выполняет G2 (spin)" }], net: [], sys: { m: "M0", g: "G3" }, sysmon: ["P0 в syscall и есть работа:", "отдаю P0 новому потоку M2"], idleM: [], wait: ["main"], hl: ["G4"] }),
    },
    {
      lines: [5],
      note: "Вытеснение: G2 занимает P1 уже 10 мс. sysmon ставит ей флаг вытеснения — его проверил бы пролог почти любой функции, но в цикле нет вызовов. Тогда sysmon шлёт потоку M1 сигнал `SIGURG`: обработчик останавливает G2 в безопасной точке и ставит в хвост глобальной очереди. P1 берёт G1.",
      scene: frame({ gq: ["G2"], p: [{ m: "M2", run: "G4", q: [], status: "M2 выполняет G4" }, { m: "M1", run: "G1", q: [], status: "M1 выполняет G1" }], net: [], sys: { m: "M0", g: "G3" }, sysmon: ["G2 на P1 дольше 10 мс:", "флаг + сигнал SIGURG"], idleM: [], wait: ["main"], hl: ["G2", "G1"] }),
    },
    {
      lines: [4],
      note: "G1 вызывает `ln.Accept()`. Сокет неблокирующий, клиента нет — горутина регистрируется в netpoller (epoll в Linux, kqueue в macOS) и паркуется. Поток M1 свободен: локальная очередь пуста, и P1 забирает из глобальной G2.",
      scene: frame({ gq: [], p: [{ m: "M2", run: "G4", q: [], status: "M2 выполняет G4" }, { m: "M1", run: "G2", q: [], status: "M1 выполняет G2 (spin)" }], net: ["G1"], sys: { m: "M0", g: "G3" }, sysmon: sleeping, idleM: [], wait: ["main"], hl: ["G1", "G2"] }),
    },
    {
      lines: [6],
      note: "`read(2)` вернулся. M0 пытается вернуть свой P0 — его уже отдали; ищет свободный P — нет. Тогда G3 встаёт в глобальную очередь, а M0 засыпает в списке простаивающих потоков: без P выполнять Go-код нельзя.",
      scene: frame({ gq: ["G3"], p: [{ m: "M2", run: "G4", q: [], status: "M2 выполняет G4" }, { m: "M1", run: "G2", q: [], status: "M1 выполняет G2 (spin)" }], net: ["G1"], sysmon: sleeping, idleM: ["M0"], wait: ["main"], hl: ["G3"] }),
    },
    {
      lines: [4],
      note: "Пришёл клиент. Сеть опрашивают свободные потоки перед кражей и sysmon, если опроса не было дольше 10 мс. Здесь все P заняты, и G1 достаёт sysmon: у него нет P, поэтому готовые горутины идут в глобальную очередь.",
      scene: frame({ gq: ["G3", "G1"], p: [{ m: "M2", run: "G4", q: [], status: "M2 выполняет G4" }, { m: "M1", run: "G2", q: [], status: "M1 выполняет G2 (spin)" }], net: [], sysmon: ["netpoll: G1 готова", "→ в глобальную очередь"], idleM: ["M0"], wait: ["main"], hl: ["G1"] }),
    },
    {
      lines: [7],
      note: "G4 закончила; у P0 пусто, и он берёт из глобальной очереди пачку: min(2, 2/GOMAXPROCS + 1, 128) = 2. G3 запускается, G1 — в локальную очередь. Даже при непустой своей очереди P раз в 61 запуск сначала смотрит в глобальную, чтобы она не голодала.",
      scene: frame({ gq: [], p: [{ m: "M2", run: "G3", q: ["G1"], status: "M2 выполняет G3" }, { m: "M1", run: "G2", q: [], status: "M1 выполняет G2 (spin)" }], net: [], sysmon: sleeping, idleM: ["M0"], wait: ["main"], hl: ["G3", "G1"] }),
    },
  ],
});
