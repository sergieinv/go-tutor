// Живой визуализатор escape analysis по выводу go build -gcflags=-m
// (POST /api/run с флагом gcflags). Каждая диагностика main.go — шаг:
// значение попадает в колонку «стек» или «куча», а пояснение говорит почему.
import { box, label, scene } from "../primitives.ts";
import type { Shape, Step } from "../types.ts";

export interface EscapeResult {
  buildFailed: boolean;
  compilerOutput?: string;
  exitCode: number;
}

export type EscapeKind = "moved" | "escapes" | "leak" | "stack";

export interface Diagnostic {
  line: number;
  col: number;
  kind: EscapeKind;
  /** Переменная или выражение из сообщения компилятора. */
  what: string;
  /** Сообщение компилятора как есть. */
  text: string;
}

export const ESCAPE_WIDTH = 680;
const ROW = 26;
const TOP = 56;
/** Больше строк в колонке не помещаем: остальные шаги идут без новых фигур. */
const MAX_ROWS = 14;
const MAX_STEPS = 40;

const lineRe = /^\.\/main\.go:(\d+):(\d+): (.+)$/;

/** Разбирает диагностику main.go; inlining только считается. */
export function parseEscape(output: string): { diags: Diagnostic[]; inlined: number } {
  const diags: Diagnostic[] = [];
  const seen = new Set<string>();
  let inlined = 0;
  for (const raw of output.split("\n")) {
    const m = lineRe.exec(raw.trim());
    if (!m || seen.has(m[0])) continue;
    seen.add(m[0]);
    const [, line, col, text] = m;
    let kind: EscapeKind;
    let what: string;
    let r: RegExpExecArray | null;
    if ((r = /^moved to heap: (.+)$/.exec(text))) [kind, what] = ["moved", r[1]];
    else if ((r = /^leaking param(?: content)?: (\S+)/.exec(text))) [kind, what] = ["leak", r[1]];
    else if ((r = /^(.+) escapes to heap$/.exec(text))) [kind, what] = ["escapes", r[1]];
    else if ((r = /^(.+) does not escape$/.exec(text))) [kind, what] = ["stack", r[1]];
    else {
      if (/^inlining call to /.test(text)) inlined++;
      continue;
    }
    diags.push({ line: Number(line), col: Number(col), kind, what, text });
  }
  diags.sort((a, b) => a.line - b.line || a.col - b.col);
  return { diags, inlined };
}

export const escapeHeight = (diags: Diagnostic[]) => {
  const rows = Math.max(diags.filter(heap).length, diags.filter((d) => !heap(d)).length, 3);
  return TOP + Math.min(rows, MAX_ROWS) * ROW + 24;
};

const heap = (d: Diagnostic) => d.kind !== "stack";

/** Читаемое имя: ~r0 — безымянный результат, ... argument — срез вариативных аргументов. */
function whatText(what: string): string {
  if (/^~r\d+$/.test(what)) return "результат вызова";
  if (what === "... argument") return "срез аргументов ...";
  return what.length > 30 ? what.slice(0, 29) + "…" : what;
}

function explain(d: Diagnostic): string {
  const w = `\`${d.what}\``;
  switch (d.kind) {
    case "moved":
      return `Переменная ${w} переезжает в кучу целиком: на неё есть указатель, который живёт дольше кадра функции (его вернули, сохранили в глобальной переменной, структуре или замыкании), — поэтому компилятор выделяет её через \`new\`, а не на стеке.`;
    case "leak":
      return `Параметр ${w} «утекает»: функция сохраняет указатель за пределами вызова (в глобальной переменной, куче или результате). Значит, то, на что он указывает, у вызывающего тоже должно жить в куче.`;
    case "escapes":
      if (d.what === "func literal") return "Замыкание уходит в кучу: его сохраняют или возвращают, а с ним — переменные, которые оно захватило.";
      if (d.what === "append")
        return "Новый массив, который выделяет `append` при росте, уходит в кучу: результат сохраняют или возвращают, либо размер не известен при компиляции.";
      if (allocates(d.what))
        return `Выражение ${w} создаёт объект в куче: указатель на него переживает функцию (возвращается, сохраняется, передаётся туда, где компилятор теряет его след) или размер не известен при компиляции и слишком велик для стека.`;
      return `Значение ${/^~r\d+$/.test(d.what) ? "результата вызова" : w} уходит в кучу при упаковке в интерфейс (\`any\`): например, аргумент \`fmt.Println\`. Компилятор не видит, что fmt не сохранит его, и копирует значение в кучу (маленькие целые и нули рантайм берёт из готовой таблицы, без выделения).`;
    case "stack":
      if (d.what === "append") return "Массив, который выделяет `append`, остаётся на стеке: срез не переживает функцию, а новый размер помещается в стековый буфер.";
      if (d.what === "... argument") return "Срез вариативных аргументов (`...`) остаётся на стеке: вызываемая функция его не сохраняет.";
      return `${w} не убегает — остаётся на стеке: компилятор доказал, что ссылка на него не переживает вызов. Такое значение освобождается вместе с кадром функции, GC о нём не знает.`;
  }
}

/** Выражение само выделяет память: &T{}, new, make, литералы среза и map. */
const allocates = (what: string) => /^(&|new\(|make\(|\[\]|map\[|string\(|\[\]byte\()/.test(what);

const kindTitle: Record<EscapeKind, string> = { moved: "в кучу", escapes: "в кучу", leak: "утечка параметра", stack: "на стеке" };

export function escapeSteps(res: EscapeResult): Step[] {
  const { diags, inlined } = parseEscape(res.compilerOutput ?? "");
  const height = escapeHeight(diags);
  const frame = (count: { stack: number; heap: number }): Shape[] =>
    scene(
      box("stack", { x: 10, y: 10, w: 320, h: height - 20, title: `Стек горутины · ${count.stack}` }),
      box("heap", { x: 350, y: 10, w: 320, h: height - 20, title: `Куча (работа для GC) · ${count.heap}`, cls: "v-waiting" }),
    );
  if (diags.length === 0) {
    return [
      {
        note: res.buildFailed
          ? "Программа не собралась — компилятору нечего показать."
          : "Компилятор не сообщил ни об одной переменной main.go: в программе нет указателей, интерфейсов и замыканий, которые надо анализировать.",
        lines: [],
        scene: [...frame({ stack: 0, heap: 0 }), label("none", ESCAPE_WIDTH / 2, height / 2, "диагностики нет", "v-caption", "middle")],
      },
    ];
  }

  const shown = diags.slice(0, MAX_STEPS);
  const heapN = diags.filter(heap).length;
  const steps: Step[] = [
    {
      note: `Компилятор с \`-gcflags=-m\` сообщил о ${diags.length} значениях: в кучу — ${heapN}, на стеке — ${diags.length - heapN}${inlined ? `; встроено вызовов (inlining) — ${inlined}` : ""}. Решение принимается при компиляции, а не во время работы. Листайте шаги по строкам кода.${diags.length > MAX_STEPS ? ` Показаны первые ${MAX_STEPS}.` : ""}`,
      lines: [],
      scene: frame({ stack: 0, heap: 0 }),
    },
  ];

  const placed: { d: Diagnostic; row: number }[] = [];
  const rows = { stack: 0, heap: 0 };
  for (const d of shown) {
    const col = heap(d) ? "heap" : "stack";
    if (rows[col] < MAX_ROWS) placed.push({ d, row: rows[col] });
    rows[col]++;
    const shapes = frame({ stack: rows.stack, heap: rows.heap });
    for (const p of placed) {
      const x = heap(p.d) ? 362 : 22;
      const y = TOP + p.row * ROW;
      const current = p.d === d;
      shapes.push(
        { type: "rect", id: `d${p.d.line}:${p.d.col}:${p.d.kind}`, x, y, w: 296, h: ROW - 4, rx: 4, cls: current ? "v-cell v-hl" : "v-cell" },
        label(`t${p.d.line}:${p.d.col}:${p.d.kind}`, x + 8, y + 16, `стр. ${p.d.line} · ${whatText(p.d.what)}`, "v-box-line"),
      );
    }
    steps.push({
      note: `Строка ${d.line}, ${kindTitle[d.kind]}: «${d.text}». ${explain(d)}`,
      lines: [d.line],
      scene: shapes,
    });
  }
  return steps;
}
