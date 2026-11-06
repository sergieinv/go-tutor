// Сценарий «Под капотом»: заголовок среза и append.
import { arrow, defineScenario, label, memory, scene, struct } from "../../../viz/src/index.ts";

const code = `s := make([]int, 3, 4)
s[0], s[1], s[2] = 1, 2, 3
t := s[1:3]
t[0] = 20
s = append(s, 4)
u := append(s, 5)
u[0] = 100
fmt.Println(s, t, u)`;

type Cells = (number | null)[];

interface Header {
  array: "A" | "B";
  offset: number;
  len: number;
  cap: number;
  hl?: string[];
}

interface Frame {
  A: Cells;
  B?: Cells;
  hlA?: number[];
  hlB?: number[];
  s: Header;
  t?: Header;
  u?: Header;
}

const ARRAY_X = 260;
const arrays = { A: { y: 40, cellW: 48 }, B: { y: 250, cellW: 44 } };
const headerY = { s: 10, t: 125, u: 240 };

function frame(f: Frame) {
  const A = memory("A", { x: ARRAY_X, y: arrays.A.y, values: f.A, title: "массив A", hl: f.hlA, cellW: arrays.A.cellW });
  const B = f.B && memory("B", { x: ARRAY_X, y: arrays.B.y, values: f.B, title: "массив B", hl: f.hlB, cellW: arrays.B.cellW });
  const parts: Parameters<typeof scene> = [A, B];
  for (const name of ["s", "t", "u"] as const) {
    const h = f[name];
    if (!h) continue;
    const hdr = struct(name, {
      x: 10,
      y: headerY[name],
      title: `${name} []int`,
      fields: [
        ["ptr", `&${h.array}[${h.offset}]`],
        ["len", h.len],
        ["cap", h.cap],
      ],
      hl: h.hl,
    });
    const target = h.array === "A" ? A : B!;
    // Заголовок ниже массива — стрелка входит в ячейку снизу, вровень — в левый край.
    const from = hdr.at("ptr");
    const top = target.at(h.offset);
    const enter = from.y > top.y + 40 ? "bottom" : h.offset === 0 ? "left" : "top";
    const to = enter === "bottom" ? target.below(h.offset) : enter === "left" ? target.at(-1) : top;
    parts.push(hdr, arrow(`${name}:ptr`, from, to, { enter }));
  }
  return scene(...parts);
}

const s3: Header = { array: "A", offset: 0, len: 3, cap: 4 };
const t: Header = { array: "A", offset: 1, len: 2, cap: 3 };
const s4: Header = { array: "A", offset: 0, len: 4, cap: 4 };
const u: Header = { array: "B", offset: 0, len: 5, cap: 8 };

export default defineScenario({
  title: "Заголовок среза и append",
  code,
  width: 640,
  height: 352,
  steps: [
    {
      lines: [1],
      note: "`make([]int, 3, 4)` выделяет массив A на 4 элемента и создаёт заголовок `s`: указатель на A[0], `len` 3, `cap` 4. Четвёртая ячейка уже выделена, но в `s` пока не видна.",
      scene: frame({ A: [0, 0, 0, null], s: { ...s3, hl: ["ptr", "len", "cap"] } }),
    },
    {
      lines: [2],
      note: "Запись по индексу идёт прямо в массив A. Заголовок `s` не меняется.",
      scene: frame({ A: [1, 2, 3, null], hlA: [0, 1, 2], s: s3 }),
    },
    {
      lines: [3],
      note: "`s[1:3]` не копирует данные: новый заголовок `t` указывает на A[1]. `len(t)` = 3 − 1 = 2, а `cap(t)` считается до конца массива: 4 − 1 = 3.",
      scene: frame({ A: [1, 2, 3, null], s: s3, t: { ...t, hl: ["ptr", "len", "cap"] } }),
    },
    {
      lines: [4],
      note: "`t[0]` — это A[1]. Запись через `t` видна и через `s`: теперь `s[1]` тоже 20.",
      scene: frame({ A: [1, 20, 3, null], hlA: [1], s: s3, t }),
    },
    {
      lines: [5],
      note: "`len(s)` = 3 < `cap(s)` = 4, место есть: `append` пишет 4 в ту же ячейку A[3] и возвращает заголовок с `len` 4. Массив тот же — новый заголовок присваиваем обратно в `s`.",
      scene: frame({ A: [1, 20, 3, 4], hlA: [3], s: { ...s4, hl: ["len"] }, t }),
    },
    {
      lines: [6],
      note: "Теперь `len(s)` = `cap(s)` = 4, места нет. Рантайм выделяет массив B вдвое больше, копирует четыре элемента и дописывает 5. `u` указывает на B, а `s` и `t` — по-прежнему на A.",
      scene: frame({ A: [1, 20, 3, 4], B: [1, 20, 3, 4, 5, null, null, null], hlB: [4], s: s4, t, u: { ...u, hl: ["ptr", "len", "cap"] } }),
    },
    {
      lines: [7],
      note: "`u[0] = 100` меняет только B. `s` и `t` этого не видят: после переаллокации у `u` своя память.",
      scene: frame({ A: [1, 20, 3, 4], B: [100, 20, 3, 4, 5, null, null, null], hlB: [0], s: s4, t, u }),
    },
    {
      lines: [8],
      note: "Итог: `s` = [1 20 3 4] и `t` = [20 3] делят массив A, `u` = [100 20 3 4 5] живёт в B. Пишет ли `append` в старый массив, зависит от `cap` — поэтому результат `append` всегда присваивают.",
      scene: scene(frame({ A: [1, 20, 3, 4], B: [100, 20, 3, 4, 5, null, null, null], s: s4, t, u }), label("legend", ARRAY_X, 150, "пунктир — выделено, но за пределами len", "v-index")),
    },
  ],
});
