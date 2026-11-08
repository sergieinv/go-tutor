// Сценарий «Под капотом»: одна и та же строка ввода проходит два пути —
// склейку в текст SQL (ввод меняет структуру запроса) и плейсхолдер
// (структура зафиксирована при подготовке, ввод — значение в слоте).
import { arrow, box, defineScenario, label, scene, struct } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `name := "' OR 1=1 --" // пришло из запроса

// УЯЗВИМО: ввод вклеен в текст SQL
q := "SELECT * FROM users WHERE name = '" + name + "'"
rows, err := db.QueryContext(ctx, q)
fmt.Println(count(rows), err) // 3 <nil>

// Плейсхолдер: текст и значение — отдельно
stmt, err := db.PrepareContext(ctx, "SELECT * FROM users WHERE name = ?")
defer stmt.Close()
rows, err = stmt.QueryContext(ctx, name)
fmt.Println(count(rows), err) // 0 <nil>`;

const INPUT = "' OR 1=1 --";

/** Ширина символа моноширинного шрифта 12px — с запасом. */
const CW = 7.5;
const CHIP_H = 26;

interface Chip {
  key: string;
  text: string;
  /** hl — пришло из ввода, muted — комментарий, slot — пустой слот параметра. */
  kind?: "hl" | "muted" | "slot";
}

const chipW = (text: string) => Math.round(text.length * CW + 14);

/** Ряд «фишек» — кусков текста или токенов. gap = 0 — фишки вплотную (одна строка). */
function chips(prefix: string, x: number, y: number, items: Chip[], gap: number) {
  const shapes: Shape[] = [];
  const pos: Record<string, { x: number; w: number }> = {};
  let cx = x;
  for (const c of items) {
    const w = chipW(c.text);
    const cls = ["v-cell", c.kind === "hl" && "v-hl", c.kind === "muted" && "v-muted-box", c.kind === "slot" && "v-cell-empty"]
      .filter(Boolean)
      .join(" ");
    shapes.push({ type: "rect", id: `${prefix}:${c.key}:r`, x: cx, y, w, h: CHIP_H, rx: 4, cls });
    shapes.push({ type: "text", id: `${prefix}:${c.key}:t`, x: cx + w / 2, y: y + 17, text: c.text, anchor: "middle", cls: "v-code" });
    pos[c.key] = { x: cx, w };
    cx += w + gap;
  }
  return { shapes, pos };
}

// Геометрия: вверху ввод, ниже — путь 1 (склейка), ещё ниже — путь 2 (плейсхолдер).
const X = 20;
const Y_INPUT = 14;
const Y1 = 76;
const Y1_BOX = 130;
const Y2 = 268;
const Y2_BOX = 316;
const BOX_X = 440;
const BOX_W = 240;
const STRUCT_W = 380;

const concat = chips(
  "p1",
  X,
  Y1,
  [
    { key: "pre", text: "SELECT * FROM users WHERE name = '" },
    { key: "in", text: INPUT, kind: "hl" },
    { key: "post", text: "'" },
  ],
  0,
);

// Ввод — ровно над своим куском в склейке: стрелка идёт вертикально.
const INPUT_X = concat.pos.in.x;
const input = chips("input", INPUT_X, Y_INPUT, [{ key: "v", text: INPUT, kind: "hl" }], 0);

const tokens1 = chips(
  "t1",
  X,
  Y1,
  [
    { key: "select", text: "SELECT" },
    { key: "star", text: "*" },
    { key: "from", text: "FROM" },
    { key: "users", text: "users" },
    { key: "where", text: "WHERE" },
    { key: "name", text: "name" },
    { key: "eq", text: "=" },
    { key: "lit", text: "''", kind: "hl" },
    { key: "or", text: "OR", kind: "hl" },
    { key: "taut", text: "1=1", kind: "hl" },
    { key: "comment", text: "--'", kind: "muted" },
  ],
  6,
);

const text2 = chips("p2", X, Y2, [{ key: "sql", text: "SELECT * FROM users WHERE name = ?" }], 0);

const tokens2 = (bound: boolean) =>
  chips(
    "t2",
    X,
    Y2,
    [
      { key: "select", text: "SELECT" },
      { key: "star", text: "*" },
      { key: "from", text: "FROM" },
      { key: "users", text: "users" },
      { key: "where", text: "WHERE" },
      { key: "name", text: "name" },
      { key: "eq", text: "=" },
      bound ? { key: "slot", text: INPUT, kind: "hl" } : { key: "slot", text: "?", kind: "slot" },
    ],
    6,
  );

type Path1 = "none" | "concat" | "sent" | "tokens" | "where" | "result";
type Path2 = "none" | "prepare" | "parsed" | "bound" | "result";

interface Frame {
  p1: Path1;
  p2: Path2;
  /** Подсветить ввод в обоих путях (итоговый шаг). */
  compare?: boolean;
}

const order1: Path1[] = ["none", "concat", "sent", "tokens", "where", "result"];
const order2: Path2[] = ["none", "prepare", "parsed", "bound", "result"];
const at1 = (f: Frame, s: Path1) => order1.indexOf(f.p1) >= order1.indexOf(s);
const at2 = (f: Frame, s: Path2) => order2.indexOf(f.p2) >= order2.indexOf(s);

function frame(f: Frame) {
  const parts: Parameters<typeof scene> = [
    label("input-cap", X, Y_INPUT + 17, "name — ввод из запроса:", "v-caption"),
    input.shapes,
    label("s1-cap", X, Y1 - 10, "Путь 1 — склейка в текст SQL", "v-caption"),
    label("s2-cap", X, Y2 - 10, "Путь 2 — плейсхолдер и значение", "v-caption"),
  ];

  // Путь 1.
  if (f.p1 === "concat" || f.p1 === "sent") {
    parts.push(concat.shapes);
    if (f.p1 === "concat") {
      const p = concat.pos.in;
      parts.push(arrow("input-to-p1", { x: INPUT_X + chipW(INPUT) / 2, y: Y_INPUT + CHIP_H }, { x: p.x + p.w / 2, y: Y1 }, { exit: "bottom", enter: "top" }));
    }
  }
  if (at1(f, "tokens")) {
    parts.push(tokens1.shapes);
    parts.push(label("t1-cap", X, Y1 + CHIP_H + 16, "подсвечено — токены из ввода; «--» превратил хвост в комментарий", "v-caption"));
  }
  if (at1(f, "sent")) {
    parts.push(
      struct("drv1", {
        x: X,
        y: Y1_BOX,
        w: STRUCT_W,
        title: "db.QueryContext(ctx, q)",
        fields: [
          ["текст", "…name = '' OR 1=1 --'"],
          ["аргументы", "нет"],
        ],
        hl: f.p1 === "sent" ? ["текст"] : undefined,
      }),
    );
  }
  if (at1(f, "where")) {
    parts.push(
      box("where1", {
        x: BOX_X,
        y: Y1_BOX,
        w: BOX_W,
        h: 84,
        title: "условие WHERE",
        lines: ["(name = '') OR (1 = 1)", "истинно для любой строки"],
        cls: f.p1 === "where" ? "v-hl" : undefined,
      }),
    );
  }
  if (at1(f, "result")) {
    parts.push(label("res1", X, Y1_BOX + 102, "результат: alice, bob, carol — все 3 строки", "v-wait"));
  }

  // Путь 2.
  if (f.p2 === "prepare") parts.push(text2.shapes);
  const bound = at2(f, "bound");
  const t2 = tokens2(bound);
  if (at2(f, "parsed")) parts.push(t2.shapes);
  if (at2(f, "prepare")) {
    const drv2 = struct("drv2", {
      x: X,
      y: Y2_BOX,
      w: STRUCT_W,
      title: bound ? "stmt.QueryContext(ctx, name)" : "db.PrepareContext(ctx, …)",
      fields: [
        ["текст", "…WHERE name = ?"],
        ["аргументы", bound ? `"${INPUT}"` : "ещё нет"],
      ],
      hl: f.p2 === "prepare" ? ["текст"] : f.p2 === "bound" ? ["аргументы"] : undefined,
    });
    parts.push(drv2);
    if (f.p2 === "bound") {
      const s = t2.pos.slot;
      parts.push(arrow("arg-to-slot", drv2.at("аргументы"), { x: s.x + s.w / 2, y: Y2 + CHIP_H }, { exit: "right", enter: "bottom" }));
    }
  }
  if (at2(f, "parsed")) {
    const lines = ["name = ?1 — план готов"];
    lines.push(bound ? `?1 = "${INPUT}"` : "слот ?1 пока пуст");
    if (at2(f, "result")) lines.push("такого имени нет");
    parts.push(
      box("where2", {
        x: BOX_X,
        y: Y2_BOX,
        w: BOX_W,
        h: 84,
        title: "условие WHERE",
        lines,
        cls: f.p2 === "parsed" || f.p2 === "bound" ? "v-hl" : undefined,
      }),
    );
  }
  if (at2(f, "result")) {
    parts.push(label("res2", X, Y2_BOX + 104, "результат: 0 строк — ввод сравнён как обычная строка", "v-caption"));
  }

  if (f.compare) {
    parts.push(label("cmp1", BOX_X + BOX_W, Y1 + 17, "4 токена SQL", "v-wait", "end"));
    parts.push(label("cmp2", BOX_X + BOX_W, Y2 + 17, "1 значение", "v-caption", "end"));
  }
  return scene(...parts);
}

export default defineScenario({
  title: "Склейка против плейсхолдера",
  code,
  width: 700,
  height: 432,
  steps: [
    {
      lines: [1],
      note: "Из запроса пришла строка `' OR 1=1 --`. Для программы это просто данные — четыре слова и кавычка. Посмотрим, что с ней станет на двух путях к базе.",
      scene: frame({ p1: "none", p2: "none" }),
    },
    {
      lines: [3, 4],
      note: "Путь 1: строка вклеивается в текст запроса. Пока это три куска — префикс, ввод и закрывающая кавычка, — и граница между вашим кодом и вводом ещё видна. Но после `+` это одна строка, и границы больше нет.",
      scene: frame({ p1: "concat", p2: "none" }),
    },
    {
      lines: [5],
      note: "Драйвер получает только текст, аргументов нет. Ни драйвер, ни СУБД не знают, какая часть текста написана программистом, а какая пришла от пользователя, — для них всё это SQL.",
      scene: frame({ p1: "sent", p2: "none" }),
    },
    {
      lines: [5],
      note: "Разбор текста. Первая кавычка ввода закрывает строковый литерал — получился пустой `''`. Дальше `OR` и `1=1` — уже не данные, а операторы. `--` начинает комментарий до конца строки и прячет закрывающую кавычку, которая иначе дала бы синтаксическую ошибку.",
      scene: frame({ p1: "tokens", p2: "none" }),
    },
    {
      lines: [5],
      note: "Ввод изменил структуру запроса: вместо одного сравнения — `(name = '') OR (1 = 1)`. Правая часть истинна всегда, и условие превращается в тавтологию. Тем же способом можно дописать `UNION SELECT` с чужой таблицей.",
      scene: frame({ p1: "where", p2: "none" }),
    },
    {
      lines: [5, 6],
      note: "Запрос возвращает всех пользователей. Если это проверка логина, атакующий вошёл под первым из них — часто под администратором.",
      scene: frame({ p1: "result", p2: "none" }),
    },
    {
      lines: [9],
      note: "Путь 2: в `PrepareContext` уходит текст с `?` — постоянная строка из кода, в которой нет ничего от пользователя. SQLite разбирает и компилирует его сразу, ещё до значений.",
      scene: frame({ p1: "result", p2: "prepare" }),
    },
    {
      lines: [9],
      note: "Структура запроса зафиксирована: одно сравнение `name` с параметром `?1`. На месте `?` — слот для значения; он может принять только значение и никогда — кусок синтаксиса.",
      scene: frame({ p1: "result", p2: "parsed" }),
    },
    {
      lines: [11],
      note: "`stmt.QueryContext(ctx, name)` передаёт ввод отдельно от текста, как аргумент (в SQLite — `sqlite3_bind_text`). Строка целиком, с кавычкой и `--`, ложится в слот как одно значение; заново текст запроса никто не разбирает.",
      scene: frame({ p1: "result", p2: "bound" }),
    },
    {
      lines: [11, 12],
      note: "СУБД ищет пользователя с именем ровно `' OR 1=1 --`, такого нет — 0 строк. Если бы такой пользователь был, он бы нашёлся: значение не искажено и не «очищено», оно просто не стало SQL.",
      scene: frame({ p1: "result", p2: "result" }),
    },
    {
      lines: [4, 11],
      note: "Итог: одна и та же строка в первом пути стала четырьмя токенами SQL, во втором — одним значением. Ручное экранирование кавычек пытается вернуть границу, стёртую склейкой, и проигрывает грамматике СУБД. А то, что плейсхолдером не передать — имена колонок, `ASC`/`DESC`, — выбирают из allowlist.",
      scene: frame({ p1: "result", p2: "result", compare: true }),
    },
  ],
});
