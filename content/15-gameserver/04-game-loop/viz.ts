// Сценарий «Под капотом»: одна и та же нагрузка (обычный тик — 10 мс,
// тяжёлый тик в 100 мс — 130 мс, пауза в 300 мс — 260 мс) на двух
// планировщиках. Накопитель держит сетку 50 мс, догоняет пропущенные тики
// подряд (не больше трёх), а лишнее отбрасывает. time.Ticker хранит в
// канале один тик и выбрасывает остальные. Расписания проверены запуском
// кода из поля code на фейковых часах и в пузыре testing/synctest.
import { defineScenario, label, scene } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `const interval = 50 * time.Millisecond
const maxCatchUp = 3

func runLoop(clock Clock, world *World) {
	next := clock.Now()
	for {
		now := clock.Now()
		due := 0
		if now >= next {
			due = int((now-next)/interval) + 1
		}
		if due > maxCatchUp {
			world.Skipped += due - maxCatchUp
			next += time.Duration(due-maxCatchUp) * interval
			due = maxCatchUp
		}
		for range due {
			world.Tick() // ввод → обновление → рассылка
			next += interval
		}
		if d := next - clock.Now(); d > 0 {
			clock.Sleep(d)
		}
	}
}

func runTicker(world *World) {
	tk := time.NewTicker(interval)
	world.Tick() // первый тик сразу
	for range tk.C {
		world.Tick()
	}
}`;

/** Тик: время начала и длительность, мс. */
type Run = [start: number, dur: number];

const loopRuns: Run[] = [
  [0, 10], [50, 10], [100, 130], [230, 10], [240, 10], [250, 10], [300, 260],
  [560, 10], [570, 10], [580, 10], [600, 10], [650, 10], [700, 10], [750, 10],
];
const tickerRuns: Run[] = [
  [0, 10], [50, 10], [100, 130], [230, 10], [250, 10], [300, 260],
  [560, 10], [600, 10], [650, 10], [700, 10], [750, 10],
];
/** Отброшенные слоты сетки и момент, с которого схема их показывает. */
const loopSkipped: Run[] = [[350, 600], [400, 600]];
const tickerDropped: Run[] = [[200, 250], [400, 600], [450, 600], [500, 600], [550, 600]];

const AX = 60; // x оси для 0 мс
const K = 0.8; // единиц схемы на миллисекунду
const x = (ms: number) => AX + ms * K;
const TRACKS = [
  { id: "L", y: 50, title: "накопитель · runLoop", runs: loopRuns, lost: loopSkipped, lostWord: "отброшено" },
  { id: "T", y: 160, title: "time.Ticker · runTicker", runs: tickerRuns, lost: tickerDropped, lostWord: "выброшено" },
];
const BAR_H = 28;
const AXIS_Y = 262;

interface Frame {
  now: number;
  /** Показывать ли отброшенные тики дорожки (решение уже принято). */
  showLost: { L: boolean; T: boolean };
  /** Подсвеченная дорожка. */
  focus?: "L" | "T";
}

function frame(f: Frame): Shape[] {
  const shapes: Shape[] = [];
  // Ось времени и подписи.
  shapes.push({ type: "path", id: "axis", d: `M${x(0)},${AXIS_Y} L${x(800)},${AXIS_Y}`, cls: "v-row" });
  for (let ms = 0; ms <= 800; ms += 100) {
    shapes.push(label(`axis:${ms}`, x(ms), AXIS_Y + 16, ms === 0 ? "0 мс" : String(ms), "v-index", "middle"));
  }
  for (const t of TRACKS) {
    const slots = Array.from({ length: 16 }, (_, i) => i * 50);
    shapes.push(label(`${t.id}:title`, 10, t.y - 10, t.title, t.id === f.focus ? "v-struct-title" : "v-caption"));
    // Сетка: засечка на каждый запланированный тик.
    for (const s of slots) {
      shapes.push({ type: "path", id: `${t.id}:grid${s}`, d: `M${x(s)},${t.y + BAR_H + 2} L${x(s)},${t.y + BAR_H + 9}`, cls: "v-ptr" });
    }
    const done = t.runs.filter(([s]) => s < f.now);
    done.forEach(([s, d], i) => {
      const heavy = d > 50;
      shapes.push({ type: "rect", id: `${t.id}:run${i}`, x: x(s), y: t.y, w: Math.max(d * K, 4), h: BAR_H, rx: 2, cls: heavy ? "v-cell v-hl" : "v-bar" });
      if (heavy) shapes.push(label(`${t.id}:runl${i}`, x(s) + 6, t.y + 18, `${d} мс`, "v-code"));
    });
    const show = t.id === "L" ? f.showLost.L : f.showLost.T;
    const lost = t.lost.filter(([, at]) => at < f.now || (at === f.now && show));
    lost.forEach(([s]) => shapes.push(label(`${t.id}:lost${s}`, x(s), t.y + BAR_H + 22, "×", "v-wait", "middle")));
    const grid = slots.filter((s) => s < f.now).length;
    shapes.push(
      label(`${t.id}:count`, x(0), t.y + BAR_H + 40, `выполнено ${done.length} из ${grid} по сетке · ${t.lostWord} ${lost.length}`, "v-label"),
    );
  }
  // Маркер «сейчас».
  const nx = x(f.now);
  shapes.push({ type: "path", id: "now", d: `M${nx},28 L${nx},${AXIS_Y}`, cls: "v-ptr v-wake" });
  shapes.push(label("now:l", nx, 18, `сейчас ${f.now} мс`, "v-caption", f.now > 700 ? "end" : "middle"));
  return scene(shapes);
}

const both = { L: true, T: true };

export default defineScenario({
  title: "Тяжёлый тик и пауза: накопитель против time.Ticker",
  code,
  width: 720,
  height: 292,
  steps: [
    {
      lines: [1, 2, 5, 18, 29],
      note: "Сетка — тик каждые 50 мс (20 TPS), засечки под дорожками. Обе версии выполняют первый тик сразу. Тик занял 10 мс — MSPT 10 из 50 мс бюджета, остаток цикл спит.",
      scene: frame({ now: 10, showLost: both }),
    },
    {
      lines: [7, 9, 10, 17, 18, 19, 21, 22],
      note: "В 50 мс наступил следующий тик: `due = 1`, тик выполнен, `next` сдвинут на 100 мс, и цикл снова спит `next - clock.Now()`. `time.Ticker` делает то же самое сам: значение в `tk.C` приходит раз в 50 мс.",
      scene: frame({ now: 60, showLost: both }),
    },
    {
      lines: [18],
      note: "Тик в 100 мс тяжёлый: 130 мс вместо 10 (пришло сто игроков, взорвался TNT, сработала сборка мусора). Пока он шёл, наступили тики 150 и 200 — мир отстаёт на два тика от настенных часов. Обе дорожки: «выполнено 3 из 5».",
      scene: frame({ now: 230, showLost: both, focus: "L" }),
    },
    {
      lines: [7, 9, 10, 17, 18, 19],
      note: "Накопитель: `now = 230`, `next = 150`, значит `due = (230 − 150) / 50 + 1 = 2`. Оба тика выполняются подряд, без сна, — 230 и 240 мс. После них `next = 250`: цикл вернулся на сетку, ни один тик не потерян, время мира снова совпадает с настенным.",
      scene: frame({ now: 250, showLost: { L: true, T: false }, focus: "L" }),
    },
    {
      lines: [30, 31],
      note: "`time.Ticker`: в канале тикера место для одного значения. Тик 150 ждал в канале и выполнен в 230, а тик 200 выброшен — канал был занят (× под засечкой). Следующий придёт по сетке в 250. Отставание в один тик осталось навсегда: время мира идёт медленнее настенного.",
      scene: frame({ now: 250, showLost: both, focus: "T" }),
    },
    {
      lines: [21, 22, 30],
      note: "Тик в 250 мс — снова по сетке на обеих дорожках. Но номер у них уже разный: у накопителя это тик 6, у тикера — тик 5.",
      scene: frame({ now: 300, showLost: both }),
    },
    {
      lines: [18, 31],
      note: "Тик в 300 мс встал на 260 мс: пауза (сохранение мира на диск в той же горутине, долгая сборка мусора, своп). За это время наступило пять тиков: 350, 400, 450, 500, 550.",
      scene: frame({ now: 560, showLost: { L: false, T: false } }),
    },
    {
      lines: [12, 13, 14, 15, 17, 18, 19],
      note: "Накопитель: `due = 5`, это больше `maxCatchUp = 3`. Два самых старых тика (350 и 400) отброшены (× под засечками): `Skipped += 2`, `next` сдвинут на 100 мс. Три оставшихся выполнены подряд в 560, 570 и 580 мс. Лимит не даёт циклу «догонять» бесконечно: 5 секунд отставания — это сто тиков подряд, мир прокрутился бы рывком, а пока он догоняет, новые команды игроков ждут.",
      scene: frame({ now: 600, showLost: { L: true, T: false }, focus: "L" }),
    },
    {
      lines: [30, 31],
      note: "`time.Ticker`: из пяти наступивших тиков в канале ждал один (350) — он выполнен в 560. Остальные четыре выброшены. Тикер не умеет ни догонять, ни сказать, сколько тиков потеряно: эту арифметику ведут сами, если время мира важно.",
      scene: frame({ now: 600, showLost: both, focus: "T" }),
    },
    {
      lines: [21, 22, 30, 31],
      note: "Итог за 800 мс (16 тиков по сетке): накопитель выполнил 14 и отбросил 2 — после короткого рывка мир вернулся на сетку. Тикер выполнил 11 и выбросил 5. Сколько догонять и когда сдаваться — решение игры: Minecraft догоняет небольшое отставание, а при отставании больше пары секунд пишет «Can't keep up!» и отбрасывает накопленные тики.",
      scene: frame({ now: 800, showLost: both }),
    },
  ],
});
