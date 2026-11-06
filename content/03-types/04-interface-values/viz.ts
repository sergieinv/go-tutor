// Сценарий «Под капотом»: интерфейсное значение как пара слов (тип, данные).
import { arrow, box, defineScenario, label, scene, struct } from "../../../viz/src/index.ts";
import type { Part, Point, Shape } from "../../../viz/src/index.ts";

const code = `var e any
e = 42
e = "go"
var buf *bytes.Buffer
e = buf
fmt.Println(e == nil)
var w io.Writer = buf
b, ok := w.(*bytes.Buffer)
_, isInt := e.(int)
w = nil
fmt.Println(b == nil, ok, isInt, w == nil)`;

type TypeID = "int" | "string" | "buf";
type ValueID = "int" | "string";

interface Frame {
  /** На что указывает e._type и e.data; undefined — nil. */
  eType?: TypeID;
  eData?: ValueID;
  eHl?: string[];
  /** Показывать ли w; withItab — w.tab указывает на itab. */
  w?: { itab: boolean; hl?: string[] };
  itab?: { hl?: string[] };
  result?: string;
}

const COL_L = 10;
const COL_M = 240;
const COL_R = 450;

const typeTitle: Record<TypeID, string> = { int: "type int", string: "type string", buf: "type *bytes.Buffer" };
const values: Record<ValueID, { title: string; line: string }> = {
  int: { title: "42", line: "копия значения" },
  string: { title: '"go"', line: "заголовок строки" },
};

function frame(f: Frame): Shape[] {
  const parts: (Part | Shape)[] = [];
  // Дескрипторы типов — в правой колонке; показываем те, на которые кто-то ссылается.
  const usedTypes = new Set<TypeID>();
  if (f.eType) usedTypes.add(f.eType);
  if (f.w?.itab || f.itab) usedTypes.add("buf");
  const types = new Map<TypeID, Part<"left" | "right" | "top" | "bottom">>();
  for (const t of usedTypes) {
    const b = box(`t-${t}`, { x: COL_R, y: 20, w: 180, h: 44, title: typeTitle[t], lines: ["дескриптор типа"] });
    types.set(t, b);
    parts.push(b);
  }

  const e = struct("e", {
    x: COL_L,
    y: 20,
    w: 176,
    title: "e any · eface",
    fields: [
      ["_type", f.eType ? "→" : "nil"],
      ["data", f.eData ? "→" : "nil"],
    ],
    hl: f.eHl,
  });
  parts.push(e);
  if (f.eType) parts.push(arrow("e:_type", e.at("_type"), types.get(f.eType)!.at("left"), { enter: "left" }));
  if (f.eData) {
    const v = values[f.eData];
    const vb = box(`v-${f.eData}`, { x: COL_M, y: 72, w: 170, h: 46, title: v.title, lines: [v.line] });
    parts.push(vb, arrow("e:data", e.at("data"), vb.at("left"), { enter: "left" }));
  }

  if (f.itab || f.w?.itab) {
    const it = struct("itab", {
      x: COL_M,
      y: 180,
      w: 190,
      title: "itab",
      fields: [
        ["Inter", "io.Writer"],
        ["Type", "→"],
        ["Fun[0]", "(*Buffer).Write"],
      ],
      hl: f.itab?.hl,
    });
    parts.push(it, arrow("itab:Type", it.at("Type"), types.get("buf")!.at("bottom"), { enter: "bottom" }));
  }

  if (f.w) {
    const w = struct("w", {
      x: COL_L,
      y: 200,
      w: 176,
      title: "w io.Writer · iface",
      fields: [
        ["tab", f.w.itab ? "→" : "nil"],
        ["data", "nil"],
      ],
      hl: f.w.hl,
    });
    parts.push(w);
    const itabLeft: Point = { x: COL_M, y: 193 };
    if (f.w.itab) parts.push(arrow("w:tab", w.at("tab"), itabLeft, { enter: "left" }));
  }

  if (f.result) parts.push(label("result", COL_L, 322, f.result, "v-label"));
  return scene(...parts);
}

export default defineScenario({
  title: "Интерфейсное значение: тип и данные",
  code,
  width: 640,
  height: 340,
  steps: [
    {
      lines: [1],
      note: "Пустой интерфейс `any` в рантайме — структура `eface` из двух машинных слов: `_type` (дескриптор динамического типа) и `data` (указатель на значение). Оба слова `nil` — это nil-интерфейс.",
      scene: frame({ eHl: ["_type", "data"] }),
    },
    {
      lines: [2],
      note: "`e = 42`: в `_type` записывается дескриптор `int`, в `data` — указатель на копию 42. Значение копируется: для констант компилятор кладёт его в статическую память, в общем случае рантайм копирует его в кучу.",
      scene: frame({ eType: "int", eData: "int", eHl: ["_type", "data"] }),
    },
    {
      lines: [3],
      note: '`e = "go"`: оба слова перезаписаны. Теперь `_type` — `string`, а `data` указывает на копию заголовка строки (указатель на байты и длина).',
      scene: frame({ eType: "string", eData: "string", eHl: ["_type", "data"] }),
    },
    {
      lines: [4, 5],
      note: "`buf` — nil-указатель типа `*bytes.Buffer`. Указатель помещается в слово `data` целиком, без копии. После `e = buf` в `data` лежит `nil`, но `_type` уже не пуст: интерфейс знает, что внутри `*bytes.Buffer`.",
      scene: frame({ eType: "buf", eHl: ["_type", "data"] }),
    },
    {
      lines: [6],
      note: "`e == nil` истинно, только если пусты оба слова. `_type` указывает на `*bytes.Buffer`, поэтому результат `false`, хотя указатель внутри — `nil`. Это и есть главная ловушка интерфейсов.",
      scene: frame({ eType: "buf", eHl: ["_type"], result: "e == nil → false: _type не nil" }),
    },
    {
      lines: [7],
      note: "Непустой интерфейс — структура `iface`: вместо `_type` в ней `tab`, указатель на `itab`. `itab` описывает пару (интерфейс, тип): `Inter` — `io.Writer`, `Type` — `*bytes.Buffer`, `Fun` — таблица методов для динамического вызова. Здесь пара известна при компиляции, и `itab` создан статически.",
      scene: frame({ eType: "buf", w: { itab: true, hl: ["tab", "data"] }, itab: { hl: ["Inter", "Type", "Fun[0]"] } }),
    },
    {
      lines: [8],
      note: "`w.(*bytes.Buffer)` проверяет динамический тип. Компилятор заранее знает `itab` пары (`io.Writer`, `*bytes.Buffer`) и просто сравнивает с ним указатель `tab`. Совпало — `ok == true`, а `b` получает содержимое `data`: тот же nil-указатель.",
      scene: frame({ eType: "buf", w: { itab: true, hl: ["tab"] }, itab: { hl: ["Inter", "Type"] }, result: "b = (*bytes.Buffer)(nil), ok = true" }),
    },
    {
      lines: [9],
      note: "`e.(int)` сравнивает `e._type` с дескриптором `int` — не совпадает. Форма с двумя результатами не паникует, а возвращает нулевое значение и `false`. Без `isInt` здесь была бы паника.",
      scene: frame({ eType: "buf", eHl: ["_type"], w: { itab: true }, result: "isInt = false" }),
    },
    {
      lines: [10],
      note: "`w = nil` обнуляет оба слова `w`. Сам `itab` остаётся в памяти: рантайм кэширует такие таблицы и не освобождает их.",
      scene: frame({ eType: "buf", w: { itab: false, hl: ["tab", "data"] }, itab: {} }),
    },
    {
      lines: [11],
      note: "Итог: `b == nil` и `ok` — `true`, `isInt` — `false`, `w == nil` — `true`, потому что у `w` теперь пусты оба слова. А `e` по-прежнему не равен `nil`: в нём лежит тип.",
      scene: frame({ eType: "buf", w: { itab: false }, itab: {}, result: "true true false true" }),
    },
  ],
});
