// Живой визуализатор select: шаги из реальной трассы запуска.
// Слева горутины ученика с состоянием, справа каждый select со счётчиками
// выбранных веток — на нём видно и ожидание, и случайный выбор среди готовых.
import { arrow, goroutine, label, scene, type GoroutineState } from "../primitives.ts";
import type { Shape, Step } from "../types.ts";
import type { Trace, TraceEvent, TraceSelect } from "./trace.ts";

const G_X = 10;
const G_W = 230;
const ROW = 44;
const TOP = 40;
const S_X = 290;
const S_W = 380;
const CASE_H = 26;
/** Больше горутин схема не рисует: события остальных остаются в тексте шагов. */
export const MAX_GOROUTINES = 10;

interface GState {
  name: string;
  state: GoroutineState;
  detail: string;
  inSelect?: number;
}

const reasonText: Record<string, string> = {
  select: "ждёт в select",
  "select (no cases)": "спит в пустом select{}",
  "chan send": "ждёт отправки в канал",
  "chan receive": "ждёт получения из канала",
  "chan receive (nil chan)": "навсегда ждёт nil-канал",
  "chan send (nil chan)": "навсегда ждёт nil-канал",
  sleep: "спит (time.Sleep)",
  "sync.WaitGroup.Wait": "ждёт WaitGroup",
  "sync.(*Cond).Wait": "ждёт sync.Cond",
  "sync.Mutex.Lock": "ждёт мьютекс",
};

const at = (line?: number) => (line ? ` · стр. ${line}` : "");
const ms = (us: number) => `${(us / 1000).toLocaleString("ru-RU", { minimumFractionDigits: 1, maximumFractionDigits: 1 })} мс`;
const short = (s: string, n: number) => (s.length > n ? s.slice(0, n - 1) + "…" : s);

/** Высота схемы для трассы — плеер выставляет её в viewBox. */
export function selectHeight(t: Trace): number {
  const gs = Math.min(t.goroutines.length, MAX_GOROUTINES) + (t.goroutines.length > MAX_GOROUTINES ? 1 : 0);
  const left = TOP + gs * ROW;
  const right = TOP + t.selects.reduce((h, s) => h + 30 + s.cases.length * CASE_H + 16, 0);
  return Math.max(left, right, 160) + 10;
}

export const SELECT_WIDTH = S_X + S_W + 10;

export function selectSteps(t: Trace, mainFile = "main.go"): Step[] {
  const order = t.goroutines.map((g) => g.id);
  const gs = new Map<number, GState>();
  const name = (id: number) => gs.get(id)?.name ?? `G${id}`;
  const counts = new Map(t.selects.map((s) => [s.id, s.cases.map(() => 0)]));
  const selById = new Map(t.selects.map((s) => [s.id, s]));
  let created = 0;
  const lineOf = (e: { file?: string; line?: number }) => (e.file === mainFile && e.line ? [e.line] : []);

  const add = (id: number, st: GState) => gs.set(id, st);
  const main = t.goroutines[0];
  if (main) add(main.id, { name: "main", state: "running", detail: "выполняется" });

  const steps: Step[] = [];
  const snapshot = (note: string, ev: TraceEvent | undefined, extra: Shape[] = [], hl?: { select: number; case: number }) => {
    steps.push({
      note,
      lines: ev ? lineOf(ev) : [],
      time: ev ? ms(ev.t) : ms(0),
      scene: draw(t, order, gs, counts, extra, hl),
    });
  };

  snapshot(
    t.events.length
      ? "Программа запущена: работает только горутина main. Листайте шаги — каждый соответствует событию из трассы."
      : "В трассе нет событий горутин и select: попробуйте программу с каналами и select.",
    undefined,
  );

  const prevByG = new Map<number, TraceEvent>();
  for (const ev of t.events) {
    const g = gs.get(ev.g);
    const prev = prevByG.get(ev.g);
    prevByG.set(ev.g, ev);
    if (g && ev.kind !== "end" && ev.kind !== "block") {
      g.state = "running";
      g.detail = "выполняется" + at(ev.line);
    }
    switch (ev.kind) {
      case "go": {
        created++;
        const child = `G${created + 1}`;
        add(ev.target!, { name: child, state: "runnable", detail: "создана" + at(ev.line) });
        snapshot(`${name(ev.g)} запускает горутину ${child} оператором \`go\` (стр. ${ev.line}). Она готова к запуску — когда начнёт, решает планировщик.`, ev);
        break;
      }
      case "select": {
        if (g) {
          g.inSelect = ev.select;
          g.detail = "в select" + at(ev.line);
        }
        snapshot(`${name(ev.g)} входит в \`select\` (стр. ${ev.line}) и проверяет все ветки разом.`, ev);
        break;
      }
      case "block": {
        if (!g) break;
        g.state = "blocked";
        g.detail = (reasonText[ev.reason ?? ""] ?? `ждёт: ${ev.reason || "?"}`) + at(ev.line);
        const why =
          ev.reason === "select"
            ? `ни одна ветка \`select\` не готова, и \`default\` нет — ${g.name} засыпает до готовности любой из веток.`
            : ev.reason === "sleep"
              ? `${g.name} засыпает в \`time.Sleep\`.`
              : `${g.name} блокируется: ${reasonText[ev.reason ?? ""] ?? ev.reason}.`;
        snapshot(ev.reason === "select" ? `${g.name}: ${why}` : why, ev);
        break;
      }
      case "unblock": {
        const target = gs.get(ev.target!);
        if (!target) break;
        target.state = "runnable";
        target.detail = "разбужена";
        const extra: Shape[] = [];
        if (gs.has(ev.g) && visible(order, ev.g) && visible(order, ev.target!)) {
          extra.push(arrow("wake", gPoint(order, ev.g, "right"), gPoint(order, ev.target!, "right"), { cls: "v-ptr v-wake", enter: "right" }));
        }
        const who = gs.has(ev.g) ? `${name(ev.g)} (стр. ${ev.line})` : "рантайм (сработал таймер или пришли данные)";
        const what = target.inSelect ? `${target.name} просыпается в \`select\`: одна из веток стала готова.` : `${target.name} снова готова к работе.`;
        snapshot(`Будит ${who}. ${what}`, ev, extra);
        break;
      }
      case "case": {
        const sel = selById.get(ev.select!);
        if (!sel) break;
        const c = counts.get(sel.id)!;
        c[ev.case]++;
        if (g) g.inSelect = undefined;
        const kase = sel.cases[ev.case];
        const ready = prev?.kind === "select" && prev.select === ev.select;
        const why =
          kase.kind === "default"
            ? "ни одна ветка не была готова — сработал `default`, горутина не ждала."
            : ready
              ? "ветка была готова сразу, ждать не пришлось."
              : "она стала готова, пока горутина ждала.";
        const extra: Shape[] = [];
        if (visible(order, ev.g)) extra.push(arrow("pick", gPoint(order, ev.g, "right"), casePoint(t.selects, sel.id, ev.case), { cls: "v-ptr v-pick", enter: "left" }));
        snapshot(`${name(ev.g)} выполняет ветку \`${kase.text}\` (стр. ${kase.line}): ${why} Счётчик ветки: ${c[ev.case]}.`, ev, extra, { select: sel.id, case: ev.case });
        break;
      }
      case "end": {
        if (!g) break;
        g.state = "done";
        g.detail = "завершилась";
        snapshot(`${g.name} завершилась.`, ev);
        break;
      }
    }
  }

  const last = steps[steps.length - 1];
  const summary = t.selects
    .filter((s) => s.counts.some((n) => n > 0))
    .map((s) => `select на стр. ${s.line}: ` + s.cases.map((c, i) => `\`${c.text}\` — ${s.counts[i]}`).join(", "))
    .join("; ");
  const cut = t.total > t.events.length ? ` Показаны первые ${t.events.length} событий из ${t.total}.` : "";
  const stuck = [...gs.values()].filter((g) => g.state === "blocked").map((g) => g.name);
  const left = stuck.length
    ? ` ${stuck.join(", ")} так и не ${stuck.length > 1 ? "дождались" : "дождалась"}: main вернул управление, и процесс завершился вместе со всеми горутинами.`
    : "";
  steps.push({
    note: `Программа завершилась за ${ms(t.durationUs)}.${left}${summary ? ` Сколько раз выбрана каждая ветка: ${summary}.` : ""}${cut}`,
    lines: [],
    time: ms(t.durationUs),
    scene: last.scene.filter((s) => s.id !== "wake" && s.id !== "pick"),
  });
  return steps;
}

const visible = (order: number[], id: number) => {
  const i = order.indexOf(id);
  return i >= 0 && i < MAX_GOROUTINES;
};

function gPoint(order: number[], id: number, side: "left" | "right") {
  const i = order.indexOf(id);
  return { x: side === "left" ? G_X : G_X + G_W, y: TOP + i * ROW + 17 };
}

function selectTop(selects: TraceSelect[], id: number) {
  let y = TOP;
  for (const s of selects) {
    if (s.id === id) return y;
    y += 30 + s.cases.length * CASE_H + 16;
  }
  return y;
}

function casePoint(selects: TraceSelect[], id: number, c: number) {
  return { x: S_X, y: selectTop(selects, id) + 30 + c * CASE_H + CASE_H / 2 };
}

function draw(
  t: Trace,
  order: number[],
  gs: Map<number, GState>,
  counts: Map<number, number[]>,
  extra: Shape[],
  hl?: { select: number; case: number },
): Shape[] {
  const parts: Shape[] = [label("g:title", G_X, 22, "Горутины", "v-caption"), label("s:title", S_X, 22, "Операторы select", "v-caption")];
  order.slice(0, MAX_GOROUTINES).forEach((id, i) => {
    const g = gs.get(id);
    if (!g) return;
    parts.push(...goroutine(`g${id}`, { x: G_X, y: TOP + i * ROW, w: G_W, name: g.name, state: g.state, detail: g.detail }).shapes);
  });
  if (order.length > MAX_GOROUTINES) {
    const hidden = order.slice(MAX_GOROUTINES).filter((id) => gs.has(id)).length;
    if (hidden > 0) parts.push(label("g:more", G_X + 10, TOP + MAX_GOROUTINES * ROW + 16, `и ещё ${hidden} — в тексте шагов`, "v-index"));
  }
  for (const s of t.selects) {
    const y = selectTop(t.selects, s.id);
    const c = counts.get(s.id)!;
    const max = Math.max(1, ...c);
    const waiting = [...gs.values()].filter((g) => g.inSelect === s.id && g.state === "blocked").map((g) => g.name);
    parts.push({ type: "rect", id: `s${s.id}:box`, x: S_X, y, w: S_W, h: 30 + s.cases.length * CASE_H + 6, rx: 8, cls: waiting.length ? "v-box v-waiting" : "v-box" });
    parts.push(label(`s${s.id}:title`, S_X + 10, y + 19, `select · стр. ${s.line}`, "v-struct-title"));
    if (waiting.length) parts.push(label(`s${s.id}:wait`, S_X + S_W - 10, y + 19, `ждёт: ${waiting.join(", ")}`, "v-wait", "end"));
    s.cases.forEach((k, i) => {
      const cy = y + 30 + i * CASE_H;
      const on = hl?.select === s.id && hl.case === i;
      parts.push({ type: "rect", id: `s${s.id}:c${i}`, x: S_X + 6, y: cy, w: S_W - 12, h: CASE_H - 4, rx: 4, cls: on ? "v-row v-hl" : "v-row" });
      // Полоса счётчика: доля от самой частой ветки этого select.
      parts.push({ type: "rect", id: `s${s.id}:bar${i}`, x: S_X + 6, y: cy + CASE_H - 7, w: ((S_W - 12) * c[i]) / max, h: 3, rx: 1, cls: "v-bar" });
      parts.push(label(`s${s.id}:t${i}`, S_X + 14, cy + 16, short(k.text, 40), "v-code"));
      parts.push(label(`s${s.id}:n${i}`, S_X + S_W - 14, cy + 16, `×${c[i]}`, "v-value", "end"));
    });
  }
  return scene(parts, extra);
}
