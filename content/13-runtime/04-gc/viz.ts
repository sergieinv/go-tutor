// Сценарий «Под капотом»: один цикл сборщика мусора Go. Фазы цикла,
// трёхцветная маркировка, гибридный барьер записи (Юаса + Дейкстра)
// при изменении указателей во время маркировки, объект, созданный
// во время маркировки, и sweep, который освобождает белые объекты.
import { arrow, box, defineScenario, label, memory, scene } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `type Obj struct{ ref *Obj }

a := &Obj{}        // A — на него ссылается стек
a.ref = &Obj{}     // B
a.ref.ref = &Obj{} // C
d := &Obj{}        // D
d = nil            // на D больше никто не ссылается
_ = d

// Куча доросла до цели — стартует цикл GC.
// Дальше код работает одновременно с маркировкой.
c := a.ref.ref  // C теперь и на стеке
a.ref.ref = nil // B больше не ссылается на C
a.ref = c       // A → C, на B никто не ссылается
e := &Obj{}     // объект создан во время маркировки
_, _ = c, e`;

type Color = "white" | "grey" | "black" | "freed";
type Obj = "A" | "B" | "C" | "D" | "E";
type Edge = "A>B" | "B>C" | "A>C";

interface Frame {
  /** 0 — цикла нет, 1..4 — фаза цикла. */
  phase: 0 | 1 | 2 | 3 | 4;
  stw: boolean;
  barrier: boolean;
  colors: Partial<Record<Obj, Color>>;
  edges: Edge[];
  /** Ячейки стека: текст. */
  stack: string[];
  queue: Obj[];
  /** Подпись внизу: что сделал барьер или рантайм. */
  action?: string;
  hl?: Obj[];
}

const R = 18;
const POS: Record<Obj, { x: number; y: number }> = {
  A: { x: 240, y: 150 },
  B: { x: 380, y: 150 },
  C: { x: 380, y: 245 },
  D: { x: 500, y: 245 },
  E: { x: 500, y: 150 },
};
const colorWord: Record<Color, string> = { white: "белый", grey: "серый", black: "чёрный", freed: "освобождён" };
const colorCls: Record<Color, string> = {
  white: "v-gc-white",
  grey: "v-gc-grey",
  black: "v-gc-black",
  freed: "v-cell-empty",
};

const PHASES = [
  ["1 · STW", "завершение sweep"],
  ["2 · маркировка", "конкурентно, 25% CPU"],
  ["3 · STW", "завершение маркировки"],
  ["4 · sweep", "конкурентно"],
];

function frame(f: Frame): Shape[] {
  const out: Shape[] = [];

  // Фазы цикла.
  PHASES.forEach(([title, sub], i) => {
    const x = 10 + i * 176;
    const active = f.phase === i + 1;
    out.push(
      { type: "rect", id: `ph${i}`, x, y: 8, w: 170, h: 42, rx: 6, cls: active ? "v-cell v-hl" : "v-cell-empty" },
      label(`ph${i}:t`, x + 8, 25, title, "v-label"),
      label(`ph${i}:s`, x + 8, 42, sub, "v-caption"),
    );
  });
  out.push(
    label("barrier", 10, 70, `барьер записи: ${f.barrier ? "включён" : "выключен"}`, "v-caption"),
    label("world", 200, 70, f.stw ? "мир остановлен (STW)" : f.phase === 0 ? "цикла GC нет" : "программа работает", f.stw ? "v-wait" : "v-caption"),
  );

  // Стек main.
  out.push(...box("stack", { x: 10, y: 80, w: 150, h: 220, title: "стек main" }).shapes);
  f.stack.forEach((text, i) => {
    out.push(
      { type: "rect", id: `st${text[0]}`, x: 22, y: 106 + i * 40, w: 126, h: 28, rx: 4, cls: "v-cell" },
      label(`st${text[0]}:t`, 32, 125 + i * 40, text, "v-code"),
    );
  });

  // Куча.
  out.push(...box("heap", { x: 176, y: 80, w: 376, h: 220, title: "куча" }).shapes);
  if (f.stack.some((s) => s.startsWith("a"))) {
    out.push(arrow("p:a", { x: 148, y: 120 }, { x: POS.A.x - R, y: POS.A.y }, { enter: "left" }));
  }
  const edgeShape: Record<Edge, () => Shape> = {
    "A>B": () => arrow("e:A>B", { x: POS.A.x + R, y: POS.A.y }, { x: POS.B.x - R, y: POS.B.y }, { enter: "left" }),
    "B>C": () => arrow("e:B>C", { x: POS.B.x, y: POS.B.y + R }, { x: POS.C.x, y: POS.C.y - R }, { exit: "bottom", enter: "top" }),
    "A>C": () => arrow("e:A>C", { x: POS.A.x, y: POS.A.y + R }, { x: POS.C.x - R, y: POS.C.y }, { exit: "bottom", enter: "left" }),
  };
  for (const e of f.edges) out.push(edgeShape[e]());
  for (const [name, color] of Object.entries(f.colors) as [Obj, Color][]) {
    const p = POS[name];
    const hl = f.hl?.includes(name);
    out.push({ type: "circle", id: `o${name}`, x: p.x, y: p.y, r: R, cls: colorCls[color] + (hl ? " v-waiting" : "") });
    const text = `${name} · ${colorWord[color]}`;
    // Подпись сверху, у C — справа (сверху в C входит стрелка от B).
    if (name === "C") out.push(label(`o${name}:t`, p.x + R + 6, p.y + 5, text, "v-label"));
    else out.push(label(`o${name}:t`, p.x, p.y - R - 8, text, "v-label", "middle"));
  }

  // Очередь серых и легенда.
  out.push(...memory("queue", { x: 568, y: 110, values: [0, 1, 2].map((i) => f.queue[i] ?? null), title: "очередь серых", cellW: 46, indexes: false }).shapes);
  const legend: [Color, string][] = [
    ["white", "белый: не найден"],
    ["grey", "серый: в очереди"],
    ["black", "чёрный: просмотрен"],
  ];
  legend.forEach(([c, text], i) => {
    out.push(
      { type: "circle", id: `lg${i}`, x: 576, y: 190 + i * 28, r: 8, cls: colorCls[c] },
      label(`lg${i}:t`, 590, 194 + i * 28, text, "v-caption"),
    );
  });

  if (f.action) out.push(label("action", 10, 326, f.action, "v-label"));
  return scene(out);
}

const base = { A: "white", B: "white", C: "white", D: "white" } as const;

export default defineScenario({
  title: "Цикл сборщика мусора: маркировка и барьер записи",
  code,
  width: 720,
  height: 340,
  steps: [
    {
      lines: [3, 4, 5, 6, 7],
      note: "Программа строит цепочку `A → B → C`; на `A` ссылается переменная `a` на стеке — это корень. На `D` после `d = nil` не ссылается никто. Цикла GC нет, барьер записи выключен, запись указателя — обычная запись в память.",
      scene: frame({ phase: 0, stw: false, barrier: false, colors: { ...base }, edges: ["A>B", "B>C"], stack: ["a → A", "d = nil"], queue: [] }),
    },
    {
      lines: [10],
      note: "Куча доросла до цели — начинается цикл. Фаза 1, sweep termination: мир останавливается (STW), рантайм дометает спаны прошлого цикла, если они остались, и на каждом P включает барьер записи и помощь в маркировке (mark assist). Пауза обычно — десятки микросекунд.",
      scene: frame({ phase: 1, stw: true, barrier: true, colors: { ...base }, edges: ["A>B", "B>C"], stack: ["a → A", "d = nil"], queue: [] }),
    },
    {
      lines: [10, 11],
      note: "Фаза 2, маркировка, идёт параллельно с программой: фоновые воркеры занимают четверть CPU — около `GOMAXPROCS/4` процессоров. Сначала корни: глобальные переменные и стеки горутин. Стек `main` просмотрен, `A` найден — он серый и встаёт в очередь. Белый — ещё не найден, серый — найден, но его указатели не просмотрены, чёрный — просмотрен.",
      scene: frame({ phase: 2, stw: false, barrier: true, colors: { ...base, A: "grey" }, edges: ["A>B", "B>C"], stack: ["a → A", "d = nil"], queue: ["A"], hl: ["A"] }),
    },
    {
      lines: [11],
      note: "Воркер берёт `A` из очереди и просматривает его поля: `B` белый — красится в серый и встаёт в очередь, `A` становится чёрным. Инвариант маркировки: чёрный объект никогда не указывает на белый — иначе белый не просмотрят и освободят.",
      scene: frame({ phase: 2, stw: false, barrier: true, colors: { ...base, A: "black", B: "grey" }, edges: ["A>B", "B>C"], stack: ["a → A", "d = nil"], queue: ["B"], hl: ["A", "B"] }),
    },
    {
      lines: [12],
      note: "Программа читает `a.ref.ref` в переменную `c`. Запись в стек идёт без барьера — так быстрее, а стек `main` уже просмотрен (он «чёрный»). `C` пока белый: на него ещё ссылается серый `B`, и воркер до него доберётся.",
      scene: frame({ phase: 2, stw: false, barrier: true, colors: { ...base, A: "black", B: "grey" }, edges: ["A>B", "B>C"], stack: ["a → A", "d = nil", "c → C"], queue: ["B"], hl: ["C"] }),
    },
    {
      lines: [13],
      note: "`a.ref.ref = nil` стирает последнюю ссылку из кучи на `C`. Без барьера `C` остался бы белым, а ссылка на него — только в уже просмотренном стеке: `C` освободили бы при живой переменной `c`. Барьер Юасы красит старое значение слота: `C` серый и в очереди.",
      scene: frame({ phase: 2, stw: false, barrier: true, colors: { ...base, A: "black", B: "grey", C: "grey" }, edges: ["A>B"], stack: ["a → A", "d = nil", "c → C"], queue: ["B", "C"], action: "барьер: shade(старое значение = C) → C серый", hl: ["C"] }),
    },
    {
      lines: [14],
      note: "`a.ref = c` пишет указатель в чёрный `A`. Барьер красит и старое значение (`B`), и новое (`C`) — это половина Дейкстры: она не даёт спрятать белый объект в чёрном. Здесь оба уже серые. На `B` больше никто не ссылается, но он серый и переживёт этот цикл — «плавающий мусор», его соберёт следующий.",
      scene: frame({ phase: 2, stw: false, barrier: true, colors: { ...base, A: "black", B: "grey", C: "grey" }, edges: ["A>C"], stack: ["a → A", "d = nil", "c → C"], queue: ["B", "C"], action: "барьер: shade(B), shade(C) — оба уже серые", hl: ["A"] }),
    },
    {
      lines: [15],
      note: "Объект, созданный во время маркировки, сразу чёрный: он заведомо жив, просматривать его незачем. Каждое выделение памяти в эту фазу ещё и платит налог: горутина, которая быстро выделяет, сама делает часть маркировки (mark assist) — так программа не обгоняет сборщик.",
      scene: frame({ phase: 2, stw: false, barrier: true, colors: { ...base, A: "black", B: "grey", C: "grey", E: "black" }, edges: ["A>C"], stack: ["a → A", "d = nil", "c → C", "e → E"], queue: ["B", "C"], hl: ["E"] }),
    },
    {
      lines: [],
      note: "Воркеры разбирают очередь: `B` (указателей больше нет) и `C` становятся чёрными. Серых не осталось — достижимое найдено. `D` так и остался белым: от корней к нему пути нет.",
      scene: frame({ phase: 2, stw: false, barrier: true, colors: { ...base, A: "black", B: "black", C: "black", E: "black" }, edges: ["A>C"], stack: ["a → A", "d = nil", "c → C", "e → E"], queue: [], hl: ["B", "C"] }),
    },
    {
      lines: [],
      note: "Фаза 3, mark termination: снова короткий STW. Рантайм убеждается, что работы не осталось, выключает воркеры и mark assist и подводит итоги: размер живой кучи станет базой для цели следующего цикла (`GOGC`).",
      scene: frame({ phase: 3, stw: true, barrier: true, colors: { ...base, A: "black", B: "black", C: "black", E: "black" }, edges: ["A>C"], stack: ["a → A", "d = nil", "c → C", "e → E"], queue: [] }),
    },
    {
      lines: [],
      note: "Фаза 4, sweep: барьер выключен, мир работает. Белый `D` освобождается — его место займут новые объекты того же размера. Никто не переезжает и не уплотняется, адреса выживших не меняются. Метки сбрасываются: в следующем цикле все снова белые, и тогда соберут `B`.",
      scene: frame({ phase: 4, stw: false, barrier: false, colors: { A: "white", B: "white", C: "white", D: "freed", E: "white" }, edges: ["A>C"], stack: ["a → A", "d = nil", "c → C", "e → E"], queue: [], action: "D освобождён; B — плавающий мусор до следующего цикла", hl: ["D"] }),
    },
  ],
});
