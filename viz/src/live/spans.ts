// Живой визуализатор «задачи и регионы»: запросы между сервисами, проброс
// дедлайна, interceptors. Программа размечает работу пакетом runtime/trace:
// trace.NewTask — запрос (вложенный запрос — дочерняя задача), trace.WithRegion /
// StartRegion — участок внутри него, trace.Log(ctx, "deadline", "осталось 30 мс") —
// заметка. Строка схемы — задача, полосы под ней — её регионы, точки — заметки.
// Шаг — одно событие: начало или конец задачи или региона, заметка.
import { label, scene } from "../primitives.ts";
import type { Shape, Step } from "../types.ts";
import type { Trace, TraceLog, TraceSpan } from "./trace.ts";

export const SPANS_WIDTH = 720;
const NAME_W = 150;
const AXIS_X = NAME_W + 20;
const AXIS_W = SPANS_WIDTH - AXIS_X - 20;
const TOP = 40;
const TASK_H = 22;
const REGION_H = 14;
const ROW_GAP = 14;
/** Больше задач схема не рисует — остальные только в тексте шагов. */
const MAX_TASKS = 10;
const MAX_STEPS = 60;

const ms = (us: number) => `${(us / 1000).toLocaleString("ru-RU", { minimumFractionDigits: 1, maximumFractionDigits: 1 })} мс`;
const cut = (s: string, n: number) => (s.length > n ? s.slice(0, n - 1) + "…" : s);

interface Row {
  task: TraceSpan;
  depth: number; // вложенность задачи — отступ имени
  regions: TraceSpan[];
  regionDepth: number; // сколько уровней регионов под строкой
  y: number;
}

/** Задачи в порядке «родитель, затем его дети» — дерево вызовов сверху вниз. */
function rowsOf(t: Trace): Row[] {
  const tasks = t.spans.filter((s) => s.kind === "task");
  const regions = t.spans.filter((s) => s.kind === "region");
  const known = new Set(tasks.map((s) => s.id));
  const children = new Map<number, TraceSpan[]>();
  for (const task of tasks) {
    const parent = known.has(task.task) ? task.task : 0;
    children.set(parent, [...(children.get(parent) ?? []), task]);
  }
  const rows: Row[] = [];
  let y = TOP;
  const walk = (parent: number, depth: number) => {
    for (const task of children.get(parent) ?? []) {
      if (rows.length >= MAX_TASKS) return;
      const own = regions.filter((r) => r.task === task.id);
      const regionDepth = own.length ? Math.max(...own.map((r) => r.depth)) + 1 : 0;
      rows.push({ task, depth, regions: own, regionDepth, y });
      y += TASK_H + regionDepth * (REGION_H + 2) + ROW_GAP;
      walk(task.id, depth + 1);
    }
  };
  walk(0, 0);
  return rows;
}

export function spansHeight(t: Trace): number {
  const rows = rowsOf(t);
  const last = rows[rows.length - 1];
  return last ? last.y + TASK_H + last.regionDepth * (REGION_H + 2) + ROW_GAP + 10 : TOP + 40;
}

type Ev = { t: number; kind: "begin" | "end"; span: TraceSpan } | { t: number; kind: "log"; log: TraceLog };

export function spansSteps(t: Trace): Step[] {
  const rows = rowsOf(t);
  const total = Math.max(t.durationUs, 1);
  const xOf = (us: number) => AXIS_X + (AXIS_W * Math.min(us, total)) / total;
  const rowOf = new Map(rows.map((r) => [r.task.id, r]));
  const taskName = (id: number) => t.spans.find((s) => s.kind === "task" && s.id === id)?.name ?? "без задачи";

  const events: Ev[] = [];
  for (const s of t.spans) {
    events.push({ t: s.start, kind: "begin", span: s });
    if (s.end >= 0) events.push({ t: s.end, kind: "end", span: s });
  }
  for (const l of t.logs) events.push({ t: l.t, kind: "log", log: l });
  // Начало раньше конца в одно и то же время, задача раньше своего региона.
  const order = { begin: 0, log: 1, end: 2 };
  events.sort((a, b) => a.t - b.t || order[a.kind] - order[b.kind]);
  const shown = events.slice(0, MAX_STEPS);

  const draw = (now: number, hl?: Ev): Shape[] => {
    const parts: Shape[] = [];
    parts.push(label("axis:0", AXIS_X, 20, "0", "v-caption"));
    parts.push(label("axis:end", AXIS_X + AXIS_W, 20, ms(total), "v-caption", "end"));
    parts.push({ type: "rect", id: "axis:now", x: xOf(now) - 1, y: 26, w: 2, h: spansHeight(t) - 36, cls: "v-bar v-hl" });
    for (const r of rows) {
      const task = r.task;
      const started = task.start <= now;
      const end = task.end >= 0 && task.end <= now ? task.end : now;
      parts.push(label(`name:${task.id}`, 10 + r.depth * 12, r.y + 15, cut(task.name, 20 - r.depth), started ? "v-label" : "v-caption"));
      if (started) {
        const lit = hl && hl.kind !== "log" && hl.span === task;
        parts.push({ type: "rect", id: `task:${task.id}`, x: xOf(task.start), y: r.y, w: Math.max(2, xOf(end) - xOf(task.start)), h: TASK_H, rx: 3, cls: lit ? "v-box v-hl" : task.end >= 0 && task.end <= now ? "v-box" : "v-box v-waiting" });
      }
      for (const reg of r.regions) {
        if (reg.start > now) continue;
        const rend = reg.end >= 0 && reg.end <= now ? reg.end : now;
        const y = r.y + TASK_H + 2 + reg.depth * (REGION_H + 2);
        const lit = hl && hl.kind !== "log" && hl.span === reg;
        parts.push({ type: "rect", id: `region:${reg.id}`, x: xOf(reg.start), y, w: Math.max(2, xOf(rend) - xOf(reg.start)), h: REGION_H, rx: 2, cls: lit ? "v-cell v-hl" : "v-cell" });
        if (xOf(rend) - xOf(reg.start) > 60) parts.push(label(`region:${reg.id}:n`, xOf(reg.start) + 4, y + 11, cut(reg.name, Math.floor((xOf(rend) - xOf(reg.start)) / 7)), "v-index"));
      }
      t.logs.forEach((l, i) => {
        if (l.task !== task.id || l.t > now) return;
        parts.push({ type: "circle", id: `log:${i}`, x: xOf(l.t), y: r.y + TASK_H / 2, r: hl?.kind === "log" && hl.log === l ? 6 : 4, cls: hl?.kind === "log" && hl.log === l ? "v-g-dot v-hl" : "v-g-dot" });
      });
    }
    return scene(parts);
  };

  const steps: Step[] = [
    {
      note: t.spans.length
        ? "Каждая строка — задача trace.NewTask (запрос в сервис; дочерний запрос — строкой ниже с отступом), полосы под ней — регионы trace.WithRegion, точки — записи trace.Log. Вертикальная линия — текущий момент; незакрытые задачи подсвечены как ждущие."
        : "В трассе нет задач и регионов: разметьте код вызовами trace.NewTask, trace.WithRegion и trace.Log из пакета runtime/trace.",
      lines: [],
      time: ms(0),
      scene: draw(0),
    },
  ];
  for (const ev of shown) {
    let note: string;
    if (ev.kind === "log") {
      note = `${taskName(ev.log.task)} · ${ev.log.category}: ${ev.log.message}`;
    } else if (ev.span.kind === "task") {
      const parent = ev.span.task && rowOf.has(ev.span.task) ? ` (вызвана из ${taskName(ev.span.task)})` : "";
      note = ev.kind === "begin" ? `Задача ${ev.span.name} началась${parent}.` : `Задача ${ev.span.name} закончилась за ${ms(ev.span.end - ev.span.start)}.`;
    } else {
      note =
        ev.kind === "begin"
          ? `${taskName(ev.span.task)}: регион «${ev.span.name}» начался.`
          : `${taskName(ev.span.task)}: регион «${ev.span.name}» закончился за ${ms(ev.span.end - ev.span.start)}.`;
    }
    steps.push({ note: `t = ${ms(ev.t)}: ${note}`, lines: [], time: ms(ev.t), scene: draw(ev.t, ev) });
  }
  const open = t.spans.filter((s) => s.end < 0);
  steps.push({
    note:
      `Программа завершилась за ${ms(t.durationUs)}.` +
      (open.length ? ` Не закрыты к выходу: ${open.map((s) => (s.kind === "task" ? `задача ${s.name}` : `регион «${s.name}»`)).join(", ")}.` : "") +
      (events.length > shown.length ? ` Показаны первые ${shown.length} событий из ${events.length}.` : "") +
      (t.spans.filter((s) => s.kind === "task").length > rows.length ? ` На схеме первые ${rows.length} задач.` : ""),
    lines: [],
    time: ms(t.durationUs),
    scene: draw(total),
  });
  return steps;
}
