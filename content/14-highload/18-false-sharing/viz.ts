// Сценарий «Под капотом»: два ядра и их кеши L1. Счётчики a и b лежат в одной
// 64-байтной линии — каждая запись забирает линию у соседнего ядра (MESI,
// упрощённо). После выравнивания b переезжает в соседнюю линию, и ядра больше
// не мешают друг другу.
import { arrow, box, defineScenario, label, memory, scene } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `type pair struct {
	a atomic.Int64 // смещение 0
	b atomic.Int64 // смещение 8: та же линия
}

type padded struct {
	a atomic.Int64
	_ [56]byte     // добивает линию 0 до 64 байт
	b atomic.Int64 // смещение 64: линия 1
}

var p pair
go func() { p.a.Add(1) }() // g0 на ядре 0
go func() { p.b.Add(1) }() // g1 на ядре 1

var q padded
go func() { q.a.Add(1) }() // g0 на ядре 0
go func() { q.b.Add(1) }() // g1 на ядре 1`;

/** Состояние копии линии в кеше ядра (MESI). */
type State = "M" | "E" | "S" | "I";
const stateName: Record<State, string> = { M: "M — изменена", E: "E — исключительная", S: "S — общая", I: "I — недействительна" };

/** Восемь ячеек по 8 байт — одна 64-байтная линия. */
type Cells = (string | null)[];

interface Cached {
  line: 0 | 1;
  state: State;
  cells: Cells;
}

interface Frame {
  /** Что лежит в кешах ядер: ни одной, одна или две линии. */
  core0?: Cached;
  core1?: Cached;
  /** Содержимое линий 0 и 1 в памяти. */
  ram: [Cells, Cells];
  /** Кто откуда получил линию на этом шаге. */
  move?: "ram0" | "ram1" | "0to1" | "1to0";
  transfers: number;
  /** Подсвеченные ячейки: «c0», «c1», «r0», «r1» + индекс. */
  hl?: string[];
}

const CELL = 36;
const CORE = [
  { x: 10, title: "ядро 0 · g0 пишет a" },
  { x: 370, title: "ядро 1 · g1 пишет b" },
];
const CORE_Y = 20;
const CORE_W = 340;
const CORE_H = 110;
const LINE_X = 26; // отступ линии внутри рамки
const RAM_Y = 250;
const RAM_LINE = [26, 386];

const empty: Cells = [null, null, null, null, null, null, null, null];
const pairLine = (a: number, b: number): Cells => [`a:${a}`, `b:${b}`, null, null, null, null, null, null];
const padLine = (a: number): Cells => [`a:${a}`, "_", "_", "_", "_", "_", "_", "_"];
const bLine = (b: number): Cells => [`b:${b}`, null, null, null, null, null, null, null];

const hlOf = (hl: string[] | undefined, key: string) => (hl ?? []).filter((h) => h.startsWith(key + ":")).map((h) => Number(h.slice(key.length + 1)));

function frame(f: Frame): Shape[] {
  const parts: Parameters<typeof scene> = [];
  [f.core0, f.core1].forEach((c, i) => {
    const x = CORE[i].x;
    parts.push(box(`core${i}`, { x, y: CORE_Y, w: CORE_W, h: CORE_H, title: CORE[i].title }));
    const title = c ? `L1 · линия ${c.line} · ${stateName[c.state]}` : "L1 · линии нет";
    const cells = c && c.state !== "I" ? c.cells : empty;
    parts.push(memory(`l1-${i}`, { x: x + LINE_X - 10, y: CORE_Y + 62, values: cells, title, cellW: CELL, cellH: 30, indexes: false, hl: hlOf(f.hl, `c${i}`), cls: c?.state === "I" ? "v-muted-box" : undefined }));
  });

  parts.push(box("ram", { x: 10, y: RAM_Y - 50, w: 700, h: 130, title: "память" }));
  f.ram.forEach((cells, i) => {
    parts.push(memory(`ram${i}`, { x: RAM_LINE[i], y: RAM_Y, values: cells, title: `линия ${i} · байты ${i * 64}–${i * 64 + 63}`, cellW: CELL, cellH: 30, indexes: false, hl: hlOf(f.hl, `r${i}`) }));
  });
  parts.push(label("bytes", 26, RAM_Y + 52, "ячейка — 8 байт; «_» — заполнитель _ [56]byte", "v-caption"));

  const coreBottom = (i: number) => ({ x: CORE[i].x + CORE_W / 2, y: CORE_Y + CORE_H });
  const ramTop = (i: number) => ({ x: RAM_LINE[i] + 4 * CELL, y: RAM_Y - 22 });
  if (f.move === "ram0" || f.move === "ram1") {
    const line = f.move === "ram0" ? 0 : 1;
    const core = line === 0 ? 0 : 1;
    parts.push(arrow("move", ramTop(line), coreBottom(core), { exit: "top", enter: "bottom" }));
  } else if (f.move === "0to1") {
    parts.push(arrow("move", coreBottom(0), coreBottom(1), { exit: "bottom", enter: "bottom", cls: "v-ptr v-wake" }));
  } else if (f.move === "1to0") {
    parts.push(arrow("move", coreBottom(1), coreBottom(0), { exit: "bottom", enter: "bottom", cls: "v-ptr v-wake" }));
  }
  parts.push(label("transfers", 360, 152, `передач линии между ядрами: ${f.transfers}`, "v-label", "middle"));
  return scene(...parts);
}

export default defineScenario({
  title: "Кеш-линии и false sharing",
  code,
  width: 720,
  height: 350,
  steps: [
    {
      lines: [1, 2, 3, 4],
      note: "`pair` — два счётчика по 8 байт подряд: `a` на смещении 0, `b` на смещении 8. Обмен между памятью и кешем идёт целыми линиями по 64 байта (на x86-64), так что оба счётчика лежат в линии 0. Кеши ядер пока пусты.",
      scene: frame({ ram: [pairLine(0, 0), empty], transfers: 0 }),
    },
    {
      lines: [13],
      note: "`g0` на ядре 0 выполняет `p.a.Add(1)`. Чтобы писать, ядро должно владеть линией единолично: оно запрашивает её целиком — вместе с чужим `b` — и держит в состоянии M (изменена, копия в памяти устарела).",
      scene: frame({ core0: { line: 0, state: "M", cells: pairLine(1, 0) }, ram: [pairLine(0, 0), empty], move: "ram0", transfers: 0, hl: ["c0:0"] }),
    },
    {
      lines: [14],
      note: "`g1` на ядре 1 пишет `p.b.Add(1)` — в другую переменную, гонки нет. Но линия та же, и она в M у ядра 0. Протокол когерентности забирает её: ядро 0 отдаёт свежие данные и помечает копию I (недействительна), ядро 1 получает линию в M.",
      scene: frame({ core0: { line: 0, state: "I", cells: pairLine(1, 0) }, core1: { line: 0, state: "M", cells: pairLine(1, 1) }, ram: [pairLine(0, 0), empty], move: "0to1", transfers: 1, hl: ["c1:1"] }),
    },
    {
      lines: [13],
      note: "Следующий `p.a.Add(1)` на ядре 0 — промах: его копия недействительна. Линия едет обратно, теперь недействительна копия ядра 1. Атомарная операция ждёт, пока линия доедет, — это десятки наносекунд вместо единиц.",
      scene: frame({ core0: { line: 0, state: "M", cells: pairLine(2, 1) }, core1: { line: 0, state: "I", cells: pairLine(1, 1) }, ram: [pairLine(0, 0), empty], move: "1to0", transfers: 2, hl: ["c0:0"] }),
    },
    {
      lines: [14],
      note: "И снова туда: каждая запись любой из горутин гоняет линию между ядрами. Это и есть false sharing — общая линия без общих данных. Только чтения так не мешают: линия может быть в S (общая) у обоих ядер сразу, ссорит их именно запись.",
      scene: frame({ core0: { line: 0, state: "I", cells: pairLine(2, 1) }, core1: { line: 0, state: "M", cells: pairLine(2, 2) }, ram: [pairLine(0, 0), empty], move: "0to1", transfers: 3, hl: ["c1:1"] }),
    },
    {
      lines: [6, 7, 8, 9, 10],
      note: "`padded`: после `a` идёт безымянное поле `_ [56]byte`, и `b` начинается со смещения 64 — в линии 1. Прочитать заполнитель нельзя, он только занимает место. Размер структуры вырос с 16 до 72 байт — плата за независимость.",
      scene: frame({ ram: [padLine(0), bLine(0)], transfers: 0 }),
    },
    {
      lines: [17],
      note: "`g0` пишет `q.a.Add(1)`: ядро 0 забирает линию 0 в M — как и раньше.",
      scene: frame({ core0: { line: 0, state: "M", cells: padLine(1) }, ram: [padLine(0), bLine(0)], move: "ram0", transfers: 0, hl: ["c0:0"] }),
    },
    {
      lines: [18],
      note: "`g1` пишет `q.b.Add(1)`: ядро 1 забирает линию 1. Линию 0 никто не трогает, копия ядра 0 остаётся в M.",
      scene: frame({ core0: { line: 0, state: "M", cells: padLine(1) }, core1: { line: 1, state: "M", cells: bLine(1) }, ram: [padLine(0), bLine(0)], move: "ram1", transfers: 0, hl: ["c1:0"] }),
    },
    {
      lines: [17, 18],
      note: "Дальше каждая запись — попадание в свой L1, между ядрами ничего не ездит, и счётчики растут независимо. В бенчмарке урока с `-cpu 4` вариант `Padded` заметно быстрее `Packed`, а при одном ядре они почти равны.",
      scene: frame({ core0: { line: 0, state: "M", cells: padLine(5) }, core1: { line: 1, state: "M", cells: bLine(5) }, ram: [padLine(0), bLine(0)], transfers: 0, hl: ["c0:0", "c1:0"] }),
    },
    {
      lines: [8],
      note: "Ловушка: размер линии — свойство процессора. У Apple M линия 128 байт, и с `[56]byte` счётчики снова делили бы одну линию. Поэтому в примере урока `CacheLine = 128`: так выравнивает рантайм Go на arm64 и `sync.Pool`, а на x86-64 128 байт заодно защищают от предвыборки соседней линии.",
      scene: frame({ core0: { line: 0, state: "M", cells: padLine(5) }, core1: { line: 1, state: "M", cells: bLine(5) }, ram: [padLine(0), bLine(0)], transfers: 0, hl: ["r0:1", "r0:2", "r0:3", "r0:4", "r0:5", "r0:6", "r0:7"] }),
    },
  ],
});
