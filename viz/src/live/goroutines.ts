// Живой визуализатор «горутины»: pipeline, fan-in/fan-out, утечки.
// Горутины группируются по стадиям — функции и строке оператора go. Шаг —
// кадр по нескольким событиям трассы: состояния горутин стадии, передачи между
// стадиями (кто кого разбудил), число живых горутин во времени.
import { arrow, box, label, scene, type Side } from "../primitives.ts";
import type { Shape, Step } from "../types.ts";
import type { Trace, TraceEvent } from "./trace.ts";

export const GOROUTINES_WIDTH = 720;
const COLS = 3;
const BOX_W = 200;
const BOX_H = 118;
const GAP_X = 50;
const GAP_Y = 40;
const TOP = 96;
/** Сколько шагов-кадров максимум: длинную трассу делим на равные куски событий. */
export const MAX_FRAMES = 40;
/** Больше стадий схема не рисует — остальные в тексте шагов. */
const MAX_GROUPS = 9;

type State = "running" | "runnable" | "send" | "recv" | "select" | "sleep" | "other" | "done";

const stateText: Record<State, string> = {
  running: "работают",
  runnable: "готовы",
  send: "ждут отправки",
  recv: "ждут получения",
  select: "ждут в select",
  sleep: "спят",
  other: "ждут (прочее)",
  done: "завершились",
};

const stateOf = (reason = ""): State =>
  reason.startsWith("chan send")
    ? "send"
    : reason.startsWith("chan receive")
      ? "recv"
      : reason.startsWith("select")
        ? "select"
        : reason === "sleep"
          ? "sleep"
          : "other";

interface Group {
  key: string;
  name: string;
  line?: number;
  members: number[];
}

/**
 * Подпись стадии: main.main → main, main.worker → worker, main.gen.func1 →
 * «go func в gen», а горутины из wg.Go / errgroup.Go — по месту вызова.
 */
const short = (fn: string) => {
  if (fn === "main.main") return "main";
  if (fn.startsWith("sync.(*WaitGroup).Go")) return "wg.Go";
  if (fn.includes("errgroup.(*Group)")) return "errgroup.Go";
  const lit = /^main\.(\w+)\.func\d+/.exec(fn);
  if (lit) return `go func в ${lit[1]}`;
  if (fn.startsWith("main.")) return fn.slice("main.".length);
  return fn.slice(fn.lastIndexOf("/") + 1);
};

const ms = (us: number) => `${(us / 1000).toLocaleString("ru-RU", { minimumFractionDigits: 1, maximumFractionDigits: 1 })} мс`;

export function goroutinesHeight(t: Trace): number {
  const groups = Math.min(groupsOf(t).length, MAX_GROUPS);
  return TOP + Math.ceil(groups / COLS) * (BOX_H + GAP_Y);
}

function groupsOf(t: Trace): Group[] {
  const byKey = new Map<string, Group>();
  for (const g of t.goroutines) {
    const key = `${g.func}@${g.line ?? 0}`;
    let grp = byKey.get(key);
    if (!grp) {
      grp = { key, name: short(g.func), line: g.line, members: [] };
      byKey.set(key, grp);
    }
    grp.members.push(g.id);
  }
  return [...byKey.values()];
}

function boxPos(i: number) {
  return { x: 10 + (i % COLS) * (BOX_W + GAP_X), y: TOP + Math.floor(i / COLS) * (BOX_H + GAP_Y) };
}

export function goroutinesSteps(t: Trace, mainFile = "main.go"): Step[] {
  const groups = groupsOf(t);
  const groupOf = new Map<number, number>();
  groups.forEach((g, i) => g.members.forEach((id) => groupOf.set(id, i)));
  const label_ = (i: number) => `${groups[i].name}${groups[i].line ? ` (стр. ${groups[i].line})` : ""}`;

  const states = new Map<number, State>();
  const where = new Map<number, number>(); // строка, где горутина ждёт
  const main = t.goroutines[0];
  if (main) states.set(main.id, "running");

  const chunk = Math.max(1, Math.ceil(t.events.length / MAX_FRAMES));
  const frames: TraceEvent[][] = [];
  for (let i = 0; i < t.events.length; i += chunk) frames.push(t.events.slice(i, i + chunk));

  const alive: number[] = [];
  const steps: Step[] = [];

  const draw = (frameStates: Map<number, State>, edges: Map<string, number>, frameIndex: number): Shape[] => {
    const parts: Shape[] = [];
    // Число живых горутин по кадрам — мини-график сверху.
    const maxAlive = Math.max(1, ...alive);
    const barW = Math.min(14, (GOROUTINES_WIDTH - 200) / Math.max(frames.length, 1));
    parts.push(label("alive:title", 10, 20, `Живых горутин: ${alive[frameIndex] ?? 1}`, "v-caption"));
    alive.forEach((n, i) => {
      const h = Math.max(2, (40 * n) / maxAlive);
      parts.push({ type: "rect", id: `alive:${i}`, x: 190 + i * barW, y: 60 - h, w: Math.max(1, barW - 2), h, rx: 1, cls: i === frameIndex ? "v-bar v-hl" : "v-bar" });
    });
    groups.slice(0, MAX_GROUPS).forEach((g, i) => {
      const { x, y } = boxPos(i);
      const counts = new Map<State, number>();
      for (const id of g.members) {
        const st = frameStates.get(id);
        if (st) counts.set(st, (counts.get(st) ?? 0) + 1);
      }
      const total = [...counts.values()].reduce((a, b) => a + b, 0);
      const blocked = (counts.get("send") ?? 0) + (counts.get("recv") ?? 0) + (counts.get("select") ?? 0) + (counts.get("other") ?? 0);
      const lines = [...counts.entries()].filter(([, n]) => n > 0).map(([st, n]) => `${stateText[st]}: ${n}`);
      parts.push(
        ...box(`grp${i}`, {
          x,
          y,
          w: BOX_W,
          h: BOX_H,
          title: g.name.length > 22 ? g.name.slice(0, 21) + "…" : g.name,
          lines: [g.line ? `go на стр. ${g.line}` : "главная горутина", ...(total ? lines.slice(0, 3) : ["ещё не запущены"])],
          cls: total === 0 ? "v-muted-box" : blocked > 0 && blocked === total - (counts.get("done") ?? 0) ? "v-waiting" : undefined,
        }).shapes,
      );
    });
    for (const [key, n] of edges) {
      const [a, b] = key.split(">").map(Number);
      if (a >= MAX_GROUPS || b >= MAX_GROUPS) continue;
      const pa = boxPos(a);
      const pb = boxPos(b);
      let exit: Side = "right";
      let enter: Side = "left";
      let from = { x: pa.x + BOX_W, y: pa.y + BOX_H / 2 };
      let to = { x: pb.x, y: pb.y + BOX_H / 2 };
      if (pb.x < pa.x) {
        exit = "left";
        enter = "right";
        from = { x: pa.x, y: pa.y + BOX_H / 2 };
        to = { x: pb.x + BOX_W, y: pb.y + BOX_H / 2 };
      } else if (pb.x === pa.x) {
        exit = pb.y > pa.y ? "bottom" : "top";
        enter = pb.y > pa.y ? "top" : "bottom";
        from = { x: pa.x + BOX_W / 2, y: pb.y > pa.y ? pa.y + BOX_H : pa.y };
        to = { x: pb.x + BOX_W / 2, y: pb.y > pa.y ? pb.y : pb.y + BOX_H };
      }
      parts.push(arrow(`edge:${key}`, from, to, { exit, enter, cls: "v-ptr" }));
      parts.push(label(`edge:${key}:n`, (from.x + to.x) / 2, (from.y + to.y) / 2 - 8, `×${n}`, "v-value", "middle"));
    }
    return scene(parts);
  };

  // Первый проход: снимок состояний и передач на конец каждого кадра.
  const snapshots: { states: Map<number, State>; edges: Map<string, number>; note: string; lines: number[]; time: string }[] = [];
  for (const frame of frames) {
    const edges = new Map<string, number>();
    let created = 0;
    let ended = 0;
    const lines = new Set<number>();
    for (const ev of frame) {
      const g = ev.g;
      // Любое событие от имени горутины значит, что она выполняется.
      if (states.has(g) && ev.kind !== "block" && ev.kind !== "end") states.set(g, "running");
      switch (ev.kind) {
        case "go":
          created++;
          states.set(ev.target!, "runnable");
          break;
        case "block":
          states.set(g, stateOf(ev.reason));
          if (ev.line) where.set(g, ev.line);
          if (ev.file === mainFile && ev.line) lines.add(ev.line);
          break;
        case "unblock": {
          states.set(ev.target!, "runnable");
          const a = groupOf.get(g);
          const b = groupOf.get(ev.target!);
          if (a !== undefined && b !== undefined && a !== b) edges.set(`${a}>${b}`, (edges.get(`${a}>${b}`) ?? 0) + 1);
          if (ev.file === mainFile && ev.line) lines.add(ev.line);
          break;
        }
        case "end":
          ended++;
          states.set(g, "done");
          break;
      }
    }
    alive.push([...states.values()].filter((st) => st !== "done").length);
    const t0 = ms(frame[0].t);
    const t1 = ms(frame[frame.length - 1].t);
    const facts: string[] = [];
    if (created) facts.push(`запущено горутин: ${created}`);
    if (ended) facts.push(`завершилось: ${ended}`);
    for (const [key, n] of edges) {
      const [a, b] = key.split(">").map(Number);
      facts.push(`${label_(a)} будит ${label_(b)}${n > 1 ? ` (×${n})` : ""}`);
    }
    const waiting = (st: State) =>
      groups
        .map((g, i) => ({ i, n: g.members.filter((id) => states.get(id) === st).length }))
        .filter((x) => x.n > 0)
        .map((x) => `${label_(x.i)} — ${x.n}`);
    const backpressure = waiting("send");
    const starving = waiting("recv");
    const note =
      `${frame.length === 1 ? `t = ${t0}` : `${t0} – ${t1}`}: ` +
      (facts.length ? facts.join("; ") + "." : "горутины работают, никто никого не будит.") +
      (backpressure.length ? ` Ждут отправки (следующая стадия не успевает или буфер полон): ${backpressure.join("; ")}.` : "") +
      (starving.length ? ` Ждут получения: ${starving.join("; ")}.` : "");
    snapshots.push({ states: new Map(states), edges, note, lines: [...lines].slice(0, 3), time: t0 });
  }

  // Второй проход — сцены: мини-графику нужны числа живых горутин по всем кадрам.
  steps.push({
    note: t.events.length
      ? "Программа запущена: работает только main. Каждый шаг — отрезок трассы: сколько горутин каждой стадии работает и чего ждёт. Стрелка — кто кого разбудил: отправитель будит ждущего получателя (значение передано), получатель — ждущего отправителя (в канале освободилось место)."
      : "В трассе нет событий горутин: запустите программу, которая создаёт горутины.",
    lines: [],
    time: ms(0),
    scene: draw(new Map(main ? [[main.id, "running" as State]] : []), new Map(), -1),
  });
  snapshots.forEach((sn, i) => steps.push({ note: sn.note, lines: sn.lines, time: sn.time, scene: draw(sn.states, sn.edges, i) }));

  // Итог: кто не завершился — кандидаты в утечки.
  const stuck = new Map<number, { n: number; state: State; line?: number }>();
  for (const [id, st] of states) {
    if (st === "done" || id === main?.id) continue;
    const gi = groupOf.get(id)!;
    const cur = stuck.get(gi) ?? { n: 0, state: st, line: where.get(id) };
    cur.n++;
    stuck.set(gi, cur);
  }
  const leakText = [...stuck.entries()]
    .map(([gi, s]) => `${label_(gi)}: ${s.n} — ${stateText[s.state]}${s.line ? ` на стр. ${s.line}` : ""}`)
    .join("; ");
  const cut = t.total > t.events.length ? ` Показаны первые ${t.events.length} событий из ${t.total}.` : "";
  steps.push({
    note:
      `Программа завершилась за ${ms(t.durationUs)}.` +
      (leakText
        ? ` Не завершились к выходу из main: ${leakText}. В долгоживущей программе (сервере) такие горутины — утечка: они держат память и никогда не проснутся.`
        : " Все горутины завершились — утечек нет.") +
      cut,
    lines: [...new Set([...stuck.values()].map((s) => s.line).filter((l): l is number => !!l))].slice(0, 3),
    time: ms(t.durationUs),
    scene: steps[steps.length - 1].scene.filter((s) => !s.id.startsWith("edge:")),
  });
  return steps;
}
