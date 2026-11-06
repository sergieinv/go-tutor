// Переиспользуемые примитивы схем. Каждый возвращает фигуры и точки привязки
// для стрелок; геометрия считается здесь, а оформление — классами из viz.css.
import type { Point, Shape } from "./types.ts";

export interface Part<K extends string | number = string | number> {
  shapes: Shape[];
  /** Точка привязки стрелки. */
  at(key: K): Point;
}

/** Собирает сцену из частей и отдельных фигур. */
export function scene(...parts: (Part | Shape | Shape[] | false | undefined)[]): Shape[] {
  const out: Shape[] = [];
  for (const p of parts) {
    if (!p) continue;
    if (Array.isArray(p)) out.push(...p);
    else if ("shapes" in p) out.push(...p.shapes);
    else out.push(p);
  }
  return out;
}

export const label = (id: string, x: number, y: number, text: string, cls = "v-label", anchor: "start" | "middle" | "end" = "start"): Shape => ({
  type: "text",
  id,
  x,
  y,
  text,
  cls,
  anchor,
});

export interface MemoryOptions {
  x: number;
  y: number;
  /** Значения ячеек; null — выделенная, но не занятая ёмкость. */
  values: (string | number | null)[];
  title?: string;
  /** Индексы подсвеченных ячеек. */
  hl?: number[];
  cellW?: number;
  cellH?: number;
  /** Подписи индексов под ячейками. */
  indexes?: boolean;
  cls?: string;
}

/**
 * Непрерывный участок памяти: массив под срезом, буфер канала, байты строки.
 * at(i) — середина верхней грани ячейки i, at(-1) — левый край по центру,
 * below(i) — под ячейкой i, ниже подписи индекса (для стрелок снизу).
 */
export function memory(id: string, o: MemoryOptions): Part<number> & { below(i: number): Point } {
  const w = o.cellW ?? 44;
  const h = o.cellH ?? 36;
  const shapes: Shape[] = [];
  if (o.title) shapes.push(label(`${id}:title`, o.x, o.y - 8, o.title, "v-caption"));
  o.values.forEach((v, i) => {
    const x = o.x + i * w;
    const empty = v === null;
    const hl = o.hl?.includes(i);
    const cls = ["v-cell", empty && "v-cell-empty", hl && "v-hl", o.cls].filter(Boolean).join(" ");
    shapes.push({ type: "rect", id: `${id}:c${i}`, x, y: o.y, w, h, cls });
    if (!empty) shapes.push({ type: "text", id: `${id}:v${i}`, x: x + w / 2, y: o.y + h / 2 + 5, text: String(v), anchor: "middle", cls: "v-value" });
    if (o.indexes !== false) shapes.push({ type: "text", id: `${id}:i${i}`, x: x + w / 2, y: o.y + h + 14, text: String(i), anchor: "middle", cls: "v-index" });
  });
  return {
    shapes,
    at: (i) => (i < 0 ? { x: o.x, y: o.y + h / 2 } : { x: o.x + i * w + w / 2, y: o.y }),
    below: (i) => ({ x: o.x + i * w + w / 2, y: o.y + h + (o.indexes === false ? 0 : 18) }),
  };
}

export interface StructOptions {
  x: number;
  y: number;
  title: string;
  fields: [name: string, value: string | number][];
  w?: number;
  /** Имена подсвеченных полей. */
  hl?: string[];
  cls?: string;
}

/** Структура с полями: заголовок среза, hchan, iface. at(field) — правый край строки поля. */
export function struct(id: string, o: StructOptions): Part<string> {
  const w = o.w ?? 150;
  const rowH = 26;
  const titleH = 26;
  const shapes: Shape[] = [
    { type: "rect", id: `${id}:box`, x: o.x, y: o.y, w, h: titleH + rowH * o.fields.length, rx: 6, cls: ["v-struct", o.cls].filter(Boolean).join(" ") },
    { type: "text", id: `${id}:title`, x: o.x + 10, y: o.y + 18, text: o.title, cls: "v-struct-title" },
  ];
  o.fields.forEach(([name, value], i) => {
    const y = o.y + titleH + i * rowH;
    const hl = o.hl?.includes(name);
    shapes.push({ type: "rect", id: `${id}:row:${name}`, x: o.x, y, w, h: rowH, cls: hl ? "v-row v-hl" : "v-row" });
    shapes.push({ type: "text", id: `${id}:name:${name}`, x: o.x + 10, y: y + 17, text: name, cls: "v-field" });
    shapes.push({ type: "text", id: `${id}:val:${name}`, x: o.x + w - 10, y: y + 17, text: String(value), anchor: "end", cls: "v-value" });
  });
  return {
    shapes,
    at: (field) => {
      const i = Math.max(0, o.fields.findIndex(([n]) => n === field));
      return { x: o.x + w, y: o.y + titleH + i * rowH + rowH / 2 };
    },
  };
}

/** С какой стороны стрелка входит в цель: top — сверху вниз, left — слева направо… */
export type Side = "top" | "bottom" | "left" | "right";

export interface ArrowOptions {
  cls?: string;
  /** Сторона цели, в которую входит стрелка. По умолчанию — по взаимному положению точек. */
  enter?: Side;
}

/** Длина наконечника: путь кончается раньше, остриё ложится точно в `to`. */
export const ARROW_HEAD = 8;

const inward: Record<Side, Point> = { top: { x: 0, y: 1 }, bottom: { x: 0, y: -1 }, left: { x: 1, y: 0 }, right: { x: -1, y: 0 } };

/**
 * Стрелка-указатель: выходит из from вправо и входит в to перпендикулярно
 * стороне enter, поэтому наконечник всегда смотрит в цель.
 */
export function arrow(id: string, from: Point, to: Point, o: ArrowOptions = {}): Shape {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const enter: Side = o.enter ?? (Math.abs(dy) < 20 ? (dx >= 0 ? "left" : "right") : dy > 0 ? "top" : "bottom");
  const dir = inward[enter];
  // Конец пути — у основания наконечника.
  const end = { x: to.x - dir.x * ARROW_HEAD, y: to.y - dir.y * ARROW_HEAD };
  const k = Math.max(30, Math.min(80, Math.hypot(dx, dy) / 2));
  const c1 = { x: from.x + k, y: from.y };
  const c2 = { x: end.x - dir.x * k, y: end.y - dir.y * k };
  const d = `M${from.x},${from.y} C${c1.x},${c1.y} ${c2.x},${c2.y} ${end.x},${end.y}`;
  return { type: "path", id, d, arrow: true, cls: o.cls ?? "v-ptr" };
}

export type GoroutineState = "running" | "runnable" | "blocked" | "done";

export interface GoroutineOptions {
  x: number;
  y: number;
  name: string;
  state: GoroutineState;
  /** Что делает горутина: «ждёт в select · стр. 23». */
  detail?: string;
  w?: number;
  hl?: boolean;
}

/** Горутина: плашка с именем и состоянием. at("right") — правый край. */
export function goroutine(id: string, o: GoroutineOptions): Part<"left" | "right"> {
  const w = o.w ?? 220;
  const h = 34;
  const shapes: Shape[] = [
    { type: "rect", id: `${id}:box`, x: o.x, y: o.y, w, h, rx: 17, cls: `v-g v-g-${o.state}${o.hl ? " v-hl" : ""}` },
    { type: "circle", id: `${id}:dot`, x: o.x + 17, y: o.y + h / 2, r: 6, cls: `v-g-dot v-g-${o.state}` },
    { type: "text", id: `${id}:name`, x: o.x + 32, y: o.y + 15, text: o.name, cls: "v-g-name" },
    { type: "text", id: `${id}:detail`, x: o.x + 32, y: o.y + 28, text: o.detail ?? stateText[o.state], cls: "v-g-detail" },
  ];
  return { shapes, at: (k) => ({ x: k === "left" ? o.x : o.x + w, y: o.y + h / 2 }) };
}

export const stateText: Record<GoroutineState, string> = {
  running: "выполняется",
  runnable: "готова к запуску",
  blocked: "заблокирована",
  done: "завершилась",
};

export interface ChannelOptions {
  x: number;
  y: number;
  label: string;
  cap: number;
  items: string[];
  closed?: boolean;
}

/** Канал: кольцевой буфер в виде ряда слотов. Для небуферизованного — пустая труба. */
export function channel(id: string, o: ChannelOptions): Part<"in" | "out"> {
  const slots = Math.max(o.cap, 1);
  const values = Array.from({ length: slots }, (_, i) => o.items[i] ?? null);
  const mem = memory(id, { x: o.x, y: o.y, values, title: `${o.label}${o.closed ? " (закрыт)" : ""}`, cellW: 34, cellH: 28, indexes: false, cls: o.cap === 0 ? "v-chan-unbuf" : "v-chan" });
  const w = slots * 34;
  return { shapes: mem.shapes, at: (k) => ({ x: k === "in" ? o.x : o.x + w, y: o.y + 14 }) };
}

export interface BoxOptions {
  x: number;
  y: number;
  w: number;
  h: number;
  title: string;
  lines?: string[];
  cls?: string;
}

/** Прямоугольник с заголовком: сервис, стадия pipeline, middleware. */
export function box(id: string, o: BoxOptions): Part<"left" | "right" | "top" | "bottom"> {
  const shapes: Shape[] = [
    { type: "rect", id: `${id}:box`, x: o.x, y: o.y, w: o.w, h: o.h, rx: 8, cls: ["v-box", o.cls].filter(Boolean).join(" ") },
    { type: "text", id: `${id}:title`, x: o.x + 10, y: o.y + 18, text: o.title, cls: "v-struct-title" },
  ];
  (o.lines ?? []).forEach((t, i) => shapes.push({ type: "text", id: `${id}:l${i}`, x: o.x + 10, y: o.y + 38 + i * 18, text: t, cls: "v-box-line" }));
  return {
    shapes,
    at: (k) =>
      k === "left" ? { x: o.x, y: o.y + o.h / 2 } : k === "right" ? { x: o.x + o.w, y: o.y + o.h / 2 } : k === "top" ? { x: o.x + o.w / 2, y: o.y } : { x: o.x + o.w / 2, y: o.y + o.h },
  };
}
