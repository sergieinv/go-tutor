// Сценарий «Под капотом»: заголовок строки, байты UTF-8, range и копирование.
import { arrow, defineScenario, label, memory, scene, struct } from "../../../viz/src/index.ts";

const code = `s := "Go, мир"
n := len(s)
c := utf8.RuneCountInString(s)
x := s[4]
for i, r := range s {
	fmt.Println(i, string(r))
}
t := s[1:4]
b := []byte(s)
b[0] = 'g'
u := s + "!"
fmt.Println(n, c, x, t, string(b), u)`;

// Байты "Go, мир" в UTF-8 — сверено запуском fmt.Printf("% x", s).
const BYTES = ["47", "6f", "2c", "20", "d0", "bc", "d0", "b8", "d1", "80"];
// Руны: символ, смещение первого байта, длина в байтах.
const RUNES: [string, number, number][] = [
  ["G", 0, 1],
  ["o", 1, 1],
  [",", 2, 1],
  ["␣", 3, 1],
  ["м", 4, 2],
  ["и", 6, 2],
  ["р", 8, 2],
];

const MEM_X = 200;
const S_Y = 50;
const CELL = 42;

interface Frame {
  hlS?: number[];
  sHl?: string[];
  info?: string;
  t?: boolean;
  b?: string[];
  hlB?: number[];
  u?: boolean;
}

function frame(f: Frame) {
  const S = memory("S", { x: MEM_X, y: S_Y, values: BYTES, hl: f.hlS, cellW: CELL });
  const s = struct("s", { x: 10, y: 10, title: "s string", fields: [["ptr", "&S[0]"], ["len", 10]], hl: f.sHl });
  const parts: Parameters<typeof scene> = [
    label("S:caption", MEM_X, 20, "S — байты строки в UTF-8 (hex)", "v-caption"),
    S,
    s,
    arrow("s:ptr", s.at("ptr"), S.at(-1), { enter: "left" }),
  ];
  // Символы над байтами, которыми они закодированы.
  for (const [ch, off, n] of RUNES) parts.push(label(`rune${off}`, MEM_X + off * CELL + (n * CELL) / 2, S_Y - 8, ch, "v-label", "middle"));
  if (f.info) parts.push(label("info", MEM_X, 140, f.info, "v-label"));
  if (f.t) {
    const t = struct("t", { x: 10, y: 120, title: "t string", fields: [["ptr", "&S[1]"], ["len", 3]] });
    parts.push(t, arrow("t:ptr", t.at("ptr"), S.below(1), { enter: "bottom" }));
  }
  if (f.b) {
    const B = memory("B", { x: MEM_X, y: 250, values: f.b, hl: f.hlB, cellW: CELL, title: "B — копия для b" });
    const b = struct("b", { x: 10, y: 220, title: "b []byte", fields: [["ptr", "&B[0]"], ["len", 10], ["cap", "≥ 10"]] });
    parts.push(B, b, arrow("b:ptr", b.at("ptr"), B.at(-1), { enter: "left" }));
  }
  if (f.u) {
    const U = memory("U", { x: MEM_X, y: 370, values: [...BYTES, "21"], hl: [10], cellW: 38, title: "U — новые байты для u" });
    const u = struct("u", { x: 10, y: 350, title: "u string", fields: [["ptr", "&U[0]"], ["len", 11]] });
    parts.push(U, u, arrow("u:ptr", u.at("ptr"), U.at(-1), { enter: "left" }));
  }
  return scene(...parts);
}

const copied = ["67", ...BYTES.slice(1)];

export default defineScenario({
  title: "Строка: заголовок и байты UTF-8",
  code,
  width: 640,
  height: 450,
  steps: [
    {
      lines: [1],
      note: "Строка — заголовок из двух полей: указатель на байты и длина. Ёмкости нет. Литерал хранится в UTF-8: латиница и пробел — по байту, каждая кириллическая буква — два байта.",
      scene: frame({ sHl: ["ptr", "len"] }),
    },
    {
      lines: [2, 3],
      note: "`len(s)` просто читает поле `len` — это 10 байтов. Символов (рун) 7, и чтобы их посчитать, `utf8.RuneCountInString` декодирует все байты.",
      scene: frame({ sHl: ["len"], info: "len(s) = 10 байтов, рун — 7" }),
    },
    {
      lines: [4],
      note: "`s[4]` возвращает байт с индексом 4 — `0xd0` = 208. Это только первая половина буквы «м», а не сама буква.",
      scene: frame({ hlS: [4], info: "s[4] = 0xd0 = 208 (byte)" }),
    },
    {
      lines: [5, 6],
      note: "`range` декодирует UTF-8 с начала. «G» — однобайтовый символ: `i` = 0, `r` = 'G' (U+0047). Следующая итерация начнётся с байта 1.",
      scene: frame({ hlS: [0], info: "i = 0, r = 'G' (U+0047), 1 байт" }),
    },
    {
      lines: [5, 6],
      note: "На смещении 4 стоит `0xd0` — признак двухбайтовой последовательности. `range` читает `d0 bc` и отдаёт `i` = 4, `r` = 'м' (U+043C). Следующее смещение — 6, индекс 5 пропущен.",
      scene: frame({ hlS: [4, 5], info: "i = 4, r = 'м' (U+043C), 2 байта" }),
    },
    {
      lines: [5, 6, 7],
      note: "Последняя итерация: `i` = 8, «р» — байты `d1 80`. Смещения шли 0, 1, 2, 3, 4, 6, 8: всего 7 итераций по 10 байтам.",
      scene: frame({ hlS: [8, 9], info: "i = 8, r = 'р' (U+0440), 2 байта" }),
    },
    {
      lines: [8],
      note: "`s[1:4]` — новый заголовок, который смотрит в те же байты: указатель на S[1], длина 3, данные не копируются. Это безопасно — строку никто не может изменить. Границы — в байтах: `s[4:5]` разрезал бы «м».",
      scene: frame({ hlS: [1, 2, 3], t: true }),
    },
    {
      lines: [9],
      note: "`[]byte(s)` обязан скопировать: срез изменяемый, и общая память позволила бы поменять строку. Рантайм выделяет массив B и копирует 10 байтов. Ёмкость может оказаться больше 10 — её округляет аллокатор.",
      scene: frame({ t: true, b: BYTES, hlB: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9] }),
    },
    {
      lines: [10],
      note: "`b[0] = 'g'` пишет `0x67` в копию. Байты S не тронуты: `s` и `t` по-прежнему видят «Go, мир» и «o, ».",
      scene: frame({ t: true, b: copied, hlB: [0] }),
    },
    {
      lines: [11, 12],
      note: "Конкатенация не дописывает в S — у строки нет ёмкости, и S неизменяем. `s + \"!\"` выделяет новый массив U на 11 байтов и копирует туда обе части. Поэтому `+=` в цикле — квадратичная сложность.",
      scene: frame({ t: true, b: copied, u: true }),
    },
  ],
});
