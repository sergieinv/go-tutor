// Сценарий «Под капотом»: как growslice выбирает новую ёмкость —
// nextslicecap, затем округление байтов до класса размера (roundupsize).
// Числа — для 64-битной платформы и Go 1.25+, проверены запуском кода.
import { arrow, box, defineScenario, label, memory, scene, struct } from "../../../viz/src/index.ts";
import type { Part, Shape } from "../../../viz/src/index.ts";

const code = `var s []int
s = append(s, 1)
s = append(s, 2, 3, 4, 5)
s = append(s, 6)
s = append(s, 7)
sink = s // s уходит в кучу
big := make([]int, 512)
big = append(big, 0)
strs := make([]string, 32)
strs = append(strs, "x")
fmt.Println(cap(s), cap(big), cap(strs))`;

interface Arr {
  /** id аллокации: новый массив — новый id, старый исчезает. */
  id: string;
  len: number;
  cap: number;
  hl?: number[];
}

interface Ladder {
  sizes: number[];
  hl?: number;
}

interface Frame {
  arr?: Arr;
  hdrHl?: string[];
  calcTitle?: string;
  calc?: string[];
  ladder?: Ladder;
  big?: { title: string; line: string; hl?: boolean };
  strs?: { title: string; line: string; hl?: boolean };
  info?: string;
}

const ARR_X = 200;
const ARR_Y = 44;
const CELL_W = 34;

function frame(f: Frame): Shape[] {
  const parts: (Part | Shape | Shape[])[] = [];
  const a = f.arr;
  const hdr = struct("s", {
    x: 10,
    y: 10,
    w: 160,
    title: "s []int",
    fields: [
      ["ptr", a ? `&${a.id}[0]` : "nil"],
      ["len", a ? a.len : 0],
      ["cap", a ? a.cap : 0],
    ],
    hl: f.hdrHl,
  });
  parts.push(hdr);
  if (a) {
    const values = Array.from({ length: a.cap }, (_, i) => (i < a.len ? i + 1 : null));
    const mem = memory(a.id, { x: ARR_X, y: ARR_Y, values, title: `массив ${a.id}: ${a.cap} × 8 Б = ${a.cap * 8} Б`, hl: a.hl, cellW: CELL_W });
    parts.push(mem, arrow("s:ptr", hdr.at("ptr"), mem.at(-1), { enter: "left" }));
  }
  if (f.calc) {
    parts.push(box("calc", { x: 10, y: 150, w: 330, h: 30 + f.calc.length * 18, title: f.calcTitle ?? "growslice", lines: f.calc }));
  }
  if (f.ladder) {
    const l = f.ladder;
    parts.push(
      memory("ladder", {
        x: 360,
        y: 170,
        values: l.sizes,
        title: "классы размера аллокатора, байт",
        hl: l.hl === undefined ? [] : [l.hl],
        cellW: 54,
        indexes: false,
      }),
    );
  }
  if (f.big) parts.push(box("big", { x: 10, y: 270, w: 300, h: 50, title: f.big.title, lines: [f.big.line], cls: f.big.hl ? "v-hl" : undefined }));
  if (f.strs) parts.push(box("strs", { x: 330, y: 270, w: 300, h: 50, title: f.strs.title, lines: [f.strs.line], cls: f.strs.hl ? "v-hl" : undefined }));
  if (f.info) parts.push(label("info", 10, 344, f.info, "v-caption"));
  return scene(...parts);
}

const a1: Arr = { id: "A", len: 1, cap: 1 };
const a2: Arr = { id: "B", len: 5, cap: 6 };
const a3: Arr = { id: "B", len: 6, cap: 6 };
const a4: Arr = { id: "C", len: 7, cap: 12 };

const small = [8, 16, 24, 32, 48];
const mid = [24, 32, 48, 64, 80];
const up = [64, 80, 96, 112, 128];
const large = [6144, 6528, 6784, 6912, 8192];
const kb = [896, 1024, 1152, 1280, 1408];

const bigMade = { title: "big []int · len 512 · cap 512", line: "массив 512 × 8 Б = 4096 Б" };
const bigGrown = { title: "big []int · len 513 · cap 848", line: "новый массив 6784 Б, старый — мусор" };
const strsMade = { title: "strs []string · len 32 · cap 32", line: "массив 32 × 16 Б = 512 Б" };
const strsGrown = { title: "strs []string · len 33 · cap 71", line: "1144 Б данных + 8 Б заголовка" };

export default defineScenario({
  title: "Рост append: формула и классы размера",
  code,
  width: 640,
  height: 356,
  steps: [
    {
      lines: [1],
      note: "Пустой срез — заголовок из трёх нулей: `ptr` = nil, `len` = 0, `cap` = 0. Массива ещё нет. Ниже будем следить, как `growslice` выбирает ёмкость: сначала формула `nextslicecap`, затем округление байтов вверх до класса размера аллокатора (`roundupsize`).",
      scene: frame({ hdrHl: ["ptr", "len", "cap"], info: "числа — для 64-битной платформы, int = 8 байт" }),
    },
    {
      lines: [2],
      note: "Места нет (`len` = `cap` = 0), вызывается `growslice(newLen = 1, oldCap = 0)`. Удвоенная старая ёмкость 0 меньше нужной длины, поэтому `nextslicecap` возвращает саму длину: 1. Это 8 байт — ровно класс 8. Массив A на 1 элемент.",
      scene: frame({
        arr: { ...a1, hl: [0] },
        hdrHl: ["ptr", "len", "cap"],
        calc: ["nextslicecap(1, 0): 1 > 2 × 0 → 1", "байт: 1 × 8 = 8", "roundupsize(8) = 8 → cap = 8 / 8 = 1"],
        ladder: { sizes: small, hl: 0 },
      }),
    },
    {
      lines: [3],
      note: "Четыре элемента сразу: нужна длина 5, а удвоение даёт только 2. Если добавляют больше, чем удвоение, формула берёт ровно нужную длину — 5, то есть 40 байт. Такого класса нет: ближайший сверху — 48 байт, и рантайм отдаёт ёмкость целиком: `cap` = 48 / 8 = 6. Старый массив A копируется в B и становится мусором.",
      scene: frame({
        arr: { ...a2, hl: [1, 2, 3, 4] },
        hdrHl: ["ptr", "len", "cap"],
        calc: ["nextslicecap(5, 1): 5 > 2 × 1 → 5", "байт: 5 × 8 = 40", "roundupsize(40) = 48 → cap = 48 / 8 = 6"],
        ladder: { sizes: mid, hl: 2 },
      }),
    },
    {
      lines: [4],
      note: "`len` 5 < `cap` 6: благодаря округлению место уже есть. `growslice` не вызывается — 6 пишется в тот же массив B, меняется только `len`. Вот зачем рантайм отдаёт весь класс размера: следующий `append` бесплатен.",
      scene: frame({ arr: { ...a3, hl: [5] }, hdrHl: ["len"], calc: ["len 5 < cap 6 — growslice не нужен"], calcTitle: "без роста", ladder: { sizes: mid } }),
    },
    {
      lines: [5],
      note: "Массив B полон. Старая ёмкость 6 < 256, поэтому ёмкость удваивается: 12, то есть 96 байт — ровно класс 96. Новый массив C, элементы копируются.",
      scene: frame({
        arr: { ...a4, hl: [6] },
        hdrHl: ["ptr", "len", "cap"],
        calc: ["nextslicecap(7, 6): 6 < 256 → 2 × 6 = 12", "байт: 12 × 8 = 96", "roundupsize(96) = 96 → cap = 12"],
        ladder: { sizes: up, hl: 2 },
      }),
    },
    {
      lines: [6],
      note: "Срез уходит в глобальную переменную — массив живёт в куче, и все шаги выше прошли через обычный `growslice`. Если бы срез не убегал, компилятор Go 1.25+ мог бы взять для первых элементов буфер на стеке (32 байта), и ёмкости вышли бы другими — например, 4 после первого `append`.",
      scene: frame({ arr: a4, info: "cap(s) = 12: формула плюс округление, а не «ровно вдвое»" }),
    },
    {
      lines: [7],
      note: "`make` выделяет ровно 512 элементов: `cap` = `len` = 512, округлять тут нечего — 4096 байт и так класс размера.",
      scene: frame({ arr: a4, big: { ...bigMade, hl: true } }),
    },
    {
      lines: [8],
      note: "Старая ёмкость 512 ≥ 256: вместо удвоения формула прибавляет `(cap + 3 × 256) / 4` = (512 + 768) / 4 = 320. Получается 832 — рост в 1,6 раза; чем больше срез, тем ближе множитель к 1,25. 832 × 8 = 6656 байт округляются до класса 6784, поэтому `cap` = 848, а не 832.",
      scene: frame({
        arr: a4,
        big: { ...bigGrown, hl: true },
        calc: ["nextslicecap(513, 512): 512 ≥ 256", "512 + (512 + 768) / 4 = 832", "832 × 8 = 6656 → roundupsize = 6784", "cap = 6784 / 8 = 848"],
        ladder: { sizes: large, hl: 2 },
      }),
    },
    {
      lines: [9],
      note: "Срез строк: элемент — заголовок строки на 16 байт, внутри указатель. 32 × 16 = 512 байт.",
      scene: frame({ arr: a4, big: bigGrown, strs: { ...strsMade, hl: true } }),
    },
    {
      lines: [10],
      note: "Удвоение даёт 64 × 16 = 1024 байта. Но в элементах есть указатели, а объект больше 512 байт — аллокатор добавляет 8-байтовый заголовок с описанием типа для сборщика мусора. 1032 байта попадают в класс 1152; минус заголовок остаётся 1144 байта, и в них помещается 71 строка, а не 64.",
      scene: frame({
        arr: a4,
        big: bigGrown,
        strs: { ...strsGrown, hl: true },
        calc: ["nextslicecap(33, 32): 32 < 256 → 64", "64 × 16 = 1024 Б + 8 Б заголовка = 1032", "roundupsize → класс 1152, данных 1144", "cap = 1144 / 16 = 71"],
        ladder: { sizes: kb, hl: 2 },
      }),
    },
    {
      lines: [11],
      note: "Итог: `12 848 71`. Ёмкость после `append` зависит от формулы роста, размера элемента, наличия в нём указателей и таблицы классов размера. Поэтому нельзя рассчитывать на конкретный `cap` — а если размер известен заранее, выделяйте его сразу: `make([]T, 0, n)`.",
      scene: frame({ arr: a4, big: bigGrown, strs: strsGrown, info: "вывод: 12 848 71 (Go 1.25+, 64 бит)" }),
    },
  ],
});
