// Живой визуализатор гонки данных по отчёту -race (POST /api/race).
// На каждую гонку три шага: где созданы горутины, предыдущий доступ,
// текущий доступ — и почему между ними нет happens-before.
import { arrow, box, goroutine, label, memory, scene } from "../primitives.ts";
import type { Step } from "../types.ts";

export interface RaceFrame {
  func: string;
  file: string;
  line: number;
}

export interface RaceAccess {
  kind: "read" | "write";
  g: number; // 0 — main
  addr: string;
  stack: RaceFrame[];
}

export interface RaceReport {
  accesses: RaceAccess[];
  goroutines: { g: number; state: string; created: RaceFrame[] }[];
}

export interface RaceResult {
  buildFailed: boolean;
  buildOutput?: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  timedOut: boolean;
  truncated: boolean;
  durationMs: number;
  races: RaceReport[];
}

export const RACE_WIDTH = 680;
export const RACE_HEIGHT = 250;
/** Больше разных гонок не показываем — обычно они повторяют одна другую. */
const MAX_RACES = 6;

const gName = (g: number) => (g === 0 ? "main" : `G${g}`);
const fnName = (f: string) => f.replace(/^main\./, "");
const kindText = { read: "чтение", write: "запись" };

/**
 * Одна и та же пара строк — одна гонка: детектор повторяет её для разных
 * горутин и видов доступа (чтение/запись, запись/запись).
 */
function unique(races: RaceReport[]): RaceReport[] {
  const seen = new Set<string>();
  return races.filter((r) => {
    const key = r.accesses
      .map((a) => `${a.stack[0]?.file}:${a.stack[0]?.line}`)
      .sort()
      .join("|");
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

export function raceSteps(res: RaceResult, mainFile = "main.go"): Step[] {
  const races = unique(res.races);
  const lineOf = (f?: RaceFrame) => (f && f.file === mainFile ? [f.line] : []);
  if (races.length === 0) {
    return [
      {
        note: res.exitCode === 0
          ? "Детектор гонок ничего не нашёл в этом запуске. Он ловит только гонки, которые реально случились при выполнении, — но если программа синхронизирована (мьютекс, канал, atomic), их и не будет."
          : "Отчётов детектора нет: программа завершилась с ошибкой раньше. Посмотрите вывод ниже.",
        lines: [],
        scene: [label("none", RACE_WIDTH / 2, RACE_HEIGHT / 2, res.exitCode === 0 ? "гонок не найдено" : "нет отчёта", "v-caption", "middle")],
      },
    ];
  }
  const steps: Step[] = [
    {
      note: `Детектор нашёл ${res.races.length === races.length ? races.length : `${res.races.length} отчётов, разных — ${races.length}`}. Гонка — это два доступа к одной памяти из разных горутин, хотя бы один из них — запись, и между ними нет синхронизации (happens-before). Листайте шаги.`,
      lines: [],
      scene: [label("intro", RACE_WIDTH / 2, RACE_HEIGHT / 2, `гонок: ${races.length}`, "v-caption", "middle")],
    },
  ];

  for (const [ri, r] of races.slice(0, MAX_RACES).entries()) {
    const [cur, prev] = r.accesses;
    if (!cur || !prev) continue;
    const n = `${ri + 1}/${Math.min(races.length, MAX_RACES)}`;
    const lanes = [prev, cur];
    const created = (g: number) => r.goroutines.find((x) => x.g === g)?.created[0];
    const mem = memory("mem", { x: 300, y: 112, values: ["?"], title: `адрес …${cur.addr.slice(-4)}`, indexes: false, cellW: 80 });

    const draw = (stage: 0 | 1 | 2) => {
      const parts: Parameters<typeof scene> = [mem];
      lanes.forEach((a, i) => {
        const y = 30 + i * 140;
        const c = created(a.g);
        parts.push(
          goroutine(`g${i}`, {
            x: 10,
            y,
            w: 230,
            name: gName(a.g),
            state: stage > i ? "running" : "runnable",
            detail: a.g === 0 ? "главная горутина" : c ? `создана: стр. ${c.line}` : "место создания не в main",
          }),
        );
        if (stage > i) {
          const top = a.stack[0];
          const caller = a.stack[1] ? ` ← ${fnName(a.stack[1].func)}:${a.stack[1].line}` : "";
          const lines = [`${kindText[a.kind]} · стр. ${top?.line ?? "?"}`, top ? `в ${fnName(top.func)}${caller}` : "вне пакета main"];
          const b = box(`acc${i}`, { x: 440, y: y - 10, w: 230, h: 76, title: i === 0 ? "предыдущий доступ" : "текущий доступ", lines: lines.slice(0, 2), cls: a.kind === "write" ? "v-waiting" : undefined });
          parts.push(b, arrow(`ptr${i}`, b.at("left"), { x: 380, y: 112 + 14 + (i === 0 ? -6 : 6) }, { exit: "left", enter: "right", cls: i === 1 ? "v-ptr v-wake" : "v-ptr" }));
        }
      });
      return scene(...parts);
    };

    steps.push({
      note: `Гонка ${n}. Участвуют ${gName(prev.g)} и ${gName(cur.g)}.${created(cur.g) ? ` ${gName(cur.g)} создана на стр. ${created(cur.g)!.line}.` : ""}${created(prev.g) ? ` ${gName(prev.g)} — на стр. ${created(prev.g)!.line}.` : ""}`,
      lines: [...lineOf(created(prev.g)), ...lineOf(created(cur.g))],
      scene: draw(0),
    });
    steps.push({
      note: `${gName(prev.g)}: ${kindText[prev.kind]} на стр. ${prev.stack[0]?.line ?? "?"}${prev.stack[0] ? ` (в \`${fnName(prev.stack[0].func)}\`)` : ""}.`,
      lines: lineOf(prev.stack[0]),
      scene: draw(1),
    });
    steps.push({
      note: `${gName(cur.g)}: ${kindText[cur.kind]} того же адреса на стр. ${cur.stack[0]?.line ?? "?"}. Между этими доступами нет ни мьютекса, ни канала, ни atomic — порядок не определён, и ${cur.kind === "write" || prev.kind === "write" ? "одна запись может потерять другую или читатель увидит «половину» значения" : "результат непредсказуем"}. Исправление: защитить обе строки одним \`sync.Mutex\`, передавать значение через канал или использовать \`sync/atomic\`.`,
      lines: [...lineOf(prev.stack[0]), ...lineOf(cur.stack[0])],
      scene: draw(2),
    });
  }
  return steps;
}
