// Сценарий «Под капотом»: повторы с растущими паузами, circuit breaker
// (closed → open → half-open → closed) и отказ по дедлайну. Четыре запроса
// к складу: A упирается в breaker, B получает быстрый отказ, C — пробный
// запрос после таймаута open, D не начинает паузу, которая переживёт дедлайн.
import { arrow, box, defineScenario, label, scene, struct } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `br := newBreaker(3, time.Second) // 3 неудачи подряд → open на 1 с
backoff := func(n int) time.Duration { return 100 * time.Millisecond << (n - 1) }

reserve := func(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for attempt := 1; ; attempt++ {
		err := br.Do(func() error { return warehouse.Reserve(ctx) })
		if err == nil || !retryable(err) {
			return err // успех, ErrOpen или «не повторять»
		}
		d := backoff(attempt) // 100, 200, 400 мс; джиттер опущен
		if dl, _ := ctx.Deadline(); time.Now().Add(d).After(dl) {
			return err // пауза переживёт дедлайн — не ждём
		}
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

warehouse.Down()
reserve(ctx, 2*time.Second)        // A: склад лежит
reserve(ctx, 2*time.Second)        // B: breaker разомкнут
time.Sleep(time.Second)            // таймаут open истёк
warehouse.Up()
reserve(ctx, 2*time.Second)        // C: пробный запрос
warehouse.Down()
reserve(ctx, 250*time.Millisecond) // D: короткий дедлайн`;

type Row = "A" | "B" | "C" | "D";
type BState = "closed" | "open" | "half";

/** Попытка: fail — ошибка склада, ok — успех, open — отказ breaker без сети, wait — в полёте. */
type Item =
  | { kind: "att"; t: number; res: "fail" | "ok" | "open" | "wait" }
  | { kind: "pause"; from: number; to: number; refused?: boolean };

const ROWS: Record<Row, { y: number; dl: string; items: Item[] }> = {
  A: {
    y: 44,
    dl: "2 с",
    items: [
      { kind: "att", t: 0, res: "fail" },
      { kind: "pause", from: 0, to: 100 },
      { kind: "att", t: 100, res: "fail" },
      { kind: "pause", from: 100, to: 300 },
      { kind: "att", t: 300, res: "fail" },
      { kind: "pause", from: 300, to: 700 },
      { kind: "att", t: 700, res: "open" },
    ],
  },
  B: { y: 110, dl: "2 с", items: [{ kind: "att", t: 0, res: "open" }] },
  C: { y: 176, dl: "2 с", items: [{ kind: "att", t: 0, res: "ok" }] },
  D: {
    y: 242,
    dl: "250 мс",
    items: [
      { kind: "att", t: 0, res: "fail" },
      { kind: "pause", from: 0, to: 100 },
      { kind: "att", t: 100, res: "fail" },
      { kind: "pause", from: 100, to: 300, refused: true },
    ],
  },
};
const ORDER: Row[] = ["A", "B", "C", "D"];

const X0 = 70;
const K = 0.45; // пикселей на миллисекунду
const ATT_W = 20;
const x = (ms: number) => X0 + ms * K;
const mark: Record<string, string> = { fail: "✕", ok: "✓", open: "⊘", wait: "…" };

interface Frame {
  /** Сколько элементов строки видно; для C можно подменить результат попытки. */
  shown: Partial<Record<Row, number>>;
  cRes?: "ok" | "wait";
  result?: Partial<Record<Row, string>>;
  deadlineD?: boolean;
  br: BState;
  failures: number;
  openNote?: string;
  /** Подсвеченный переход: id стрелки. */
  edge?: "trip" | "timeout" | "probe-ok";
  up: boolean;
  calls: number;
  fast: number;
  /** Подсвеченный элемент: «A:6». */
  hl?: string;
}

function rowShapes(f: Frame): Shape[] {
  const out: Shape[] = [label("cap-t", X0, 20, "время от начала запроса →, паузы в мс", "v-caption")];
  for (const name of ORDER) {
    const r = ROWS[name];
    const n = f.shown[name] ?? 0;
    out.push(label(`row:${name}`, 12, r.y + 16, name, n ? "v-label" : "v-caption"));
    out.push(label(`row:${name}:dl`, 12, r.y + 34, r.dl, "v-caption"));
    r.items.slice(0, n).forEach((it, i) => {
      const id = `${name}:${i}`;
      const lit = f.hl === id;
      if (it.kind === "att") {
        const res = name === "C" && f.cRes ? f.cRes : it.res;
        const cls = res === "open" || res === "wait" ? "v-cell-empty" : res === "fail" ? "v-cell v-waiting" : "v-cell";
        out.push({ type: "rect", id: `${id}:box`, x: x(it.t), y: r.y, w: ATT_W, h: 22, rx: 3, cls: lit ? `${cls} v-hl` : cls });
        out.push({ type: "text", id: `${id}:mark`, x: x(it.t) + ATT_W / 2, y: r.y + 16, text: mark[res], anchor: "middle", cls: "v-value" });
      } else {
        const x1 = x(it.from) + ATT_W;
        const x2 = x(it.to);
        out.push({ type: "rect", id: `${id}:bar`, x: x1, y: r.y + 8, w: x2 - x1, h: 6, cls: it.refused ? "v-cell-empty" : lit ? "v-bar v-hl" : "v-bar" });
        if (it.refused) out.push(label(`${id}:lbl`, x(250) - 5, r.y - 5, `${it.to - it.from}?`, "v-wait", "end"));
        else out.push(label(`${id}:lbl`, (x1 + x2) / 2, r.y - 5, String(it.to - it.from), "v-index", "middle"));
      }
    });
    if (name === "D" && f.deadlineD) {
      out.push({ type: "rect", id: "D:deadline", x: x(250) - 1.5, y: r.y - 14, w: 3, h: 42, cls: "v-g-dot v-g-blocked" });
      out.push(label("D:deadline:l", x(250) + 6, r.y - 5, "дедлайн", "v-wait"));
    }
    const res = f.result?.[name];
    if (res) out.push(label(`row:${name}:res`, X0, r.y + 42, res, "v-caption"));
  }
  return out;
}

const BX = { closed: { x: 535, y: 44 }, open: { x: 465, y: 184 }, half: { x: 610, y: 184 } };
const BW = 105;
const BH = 56;

function breakerShapes(f: Frame): Parameters<typeof scene> {
  const cls = (s: BState) => (f.br === s ? "v-hl" : "v-muted-box");
  const closedLines = [`неудач: ${f.failures}/3`];
  const parts: Parameters<typeof scene> = [
    label("br:cap", 465, 20, "circuit breaker", "v-caption"),
    box("br:closed", { ...BX.closed, w: BW, h: BH, title: "closed", lines: closedLines, cls: cls("closed") }),
    box("br:open", { ...BX.open, w: BW, h: BH, title: "open", lines: [f.openNote ?? "отказ сразу"], cls: cls("open") }),
    box("br:half", { ...BX.half, w: BW, h: BH, title: "half-open", lines: ["одна проба"], cls: cls("half") }),
  ];
  const e = (id: "trip" | "timeout" | "probe-ok" | "probe-fail") => (f.edge === id ? "v-ptr v-waiting" : "v-ptr");
  parts.push(arrow("br:e:trip", { x: BX.closed.x + 20, y: BX.closed.y + BH }, { x: BX.open.x + BW / 2, y: BX.open.y }, { exit: "bottom", enter: "top", cls: e("trip") }));
  parts.push(label("br:l:trip", 465, 140, "3 неудачи", "v-index"));
  parts.push(arrow("br:e:timeout", { x: BX.open.x + BW, y: BX.open.y + BH / 2 }, { x: BX.half.x, y: BX.half.y + BH / 2 }, { exit: "right", enter: "left", cls: e("timeout") }));
  parts.push(label("br:l:timeout", (BX.open.x + BW + BX.half.x) / 2, BX.open.y + BH + 14, "1 с", "v-index", "middle"));
  parts.push(arrow("br:e:probe-ok", { x: BX.half.x + BW / 2 + 10, y: BX.half.y }, { x: BX.closed.x + BW, y: BX.closed.y + BH / 2 }, { exit: "top", enter: "right", cls: e("probe-ok") }));
  parts.push(label("br:l:probe-ok", 600, 140, "проба ок", "v-index"));
  parts.push(arrow("br:e:probe-fail", { x: BX.half.x + BW / 2, y: BX.half.y + BH }, { x: BX.open.x + BW / 2, y: BX.open.y + BH }, { exit: "bottom", enter: "bottom", cls: e("probe-fail") }));
  parts.push(label("br:l:probe-fail", (BX.open.x + BX.half.x + BW) / 2, 312, "проба упала", "v-index", "middle"));
  return parts;
}

function frame(f: Frame): Shape[] {
  return scene(
    rowShapes(f),
    ...breakerShapes(f),
    box("wh", { x: 10, y: 312, w: 200, h: 60, title: "склад", lines: [f.up ? "работает" : "лежит: Unavailable"], cls: f.up ? undefined : "v-waiting" }),
    struct("stats", {
      x: 225,
      y: 312,
      w: 215,
      title: "счётчики",
      fields: [
        ["запросов к складу", f.calls],
        ["быстрых отказов", f.fast],
      ],
    }),
  );
}

export default defineScenario({
  title: "Повторы, circuit breaker и дедлайн",
  code,
  width: 720,
  height: 400,
  steps: [
    {
      lines: [1, 2, 24],
      note: "Breaker создан замкнутым (`closed`): все вызовы идут к складу, а он считает неудачи подряд. Пауза между попытками удваивается: 100, 200, 400 мс. Склад лежит и на любой запрос отвечает `Unavailable`.",
      scene: frame({ shown: {}, br: "closed", failures: 0, up: false, calls: 0, fast: 0 }),
    },
    {
      lines: [25, 5, 8],
      note: "Запрос A с дедлайном 2 с. Первая попытка проходит через `br.Do` к складу и получает `Unavailable`. Breaker записывает неудачу: 1 из 3.",
      scene: frame({ shown: { A: 1 }, br: "closed", failures: 1, up: false, calls: 1, fast: 0, hl: "A:0" }),
    },
    {
      lines: [9, 12, 13, 17, 8],
      note: "`Unavailable` повторяемый, пауза `backoff(1)` = 100 мс укладывается в дедлайн. После неё вторая попытка — снова отказ: 2 из 3.",
      scene: frame({ shown: { A: 3 }, br: "closed", failures: 2, up: false, calls: 2, fast: 0, hl: "A:2" }),
    },
    {
      lines: [12, 17, 8],
      note: "Пауза 200 мс, третья попытка, третья неудача подряд — порог достигнут, breaker размыкается (`open`) на 1 с. Пока он открыт, вызовы к складу не идут вовсе.",
      scene: frame({ shown: { A: 5 }, br: "open", failures: 3, edge: "trip", up: false, calls: 3, fast: 0, hl: "A:4" }),
    },
    {
      lines: [12, 13, 17, 8, 9, 10],
      note: "Пауза 400 мс ещё укладывается в 2 с, и четвёртая попытка вызывает `br.Do`. Breaker открыт: он сразу возвращает `ErrOpen`, не трогая сеть. `retryable(ErrOpen)` — `false` (это не gRPC-код), и `reserve` возвращает ошибку через 0,7 с, а не через 2.",
      scene: frame({ shown: { A: 7 }, result: { A: "итог: ErrOpen за 0,7 с" }, br: "open", failures: 3, up: false, calls: 3, fast: 1, hl: "A:6" }),
    },
    {
      lines: [26, 8, 10],
      note: "Запрос B приходит, пока breaker открыт, и получает `ErrOpen` мгновенно. Пользователь узнаёт об отказе сразу, а лежащий склад не получает ни одного лишнего запроса — ради этого breaker и нужен.",
      scene: frame({ shown: { A: 7, B: 1 }, result: { A: "итог: ErrOpen за 0,7 с", B: "итог: ErrOpen сразу, без сети" }, br: "open", failures: 3, up: false, calls: 3, fast: 2, hl: "B:0" }),
    },
    {
      lines: [27, 28],
      note: "Прошла секунда: таймаут `open` истёк, а склад тем временем поднялся. Breaker об этом ещё не знает — проверить можно только запросом. Следующий вызов переведёт его в `half-open`.",
      scene: frame({ shown: { A: 7, B: 1 }, result: { A: "итог: ErrOpen за 0,7 с", B: "итог: ErrOpen сразу, без сети" }, br: "open", failures: 3, openNote: "таймаут истёк", up: true, calls: 3, fast: 2 }),
    },
    {
      lines: [29, 8],
      note: "Запрос C: breaker в `half-open` пропускает одну пробную попытку к складу. Остальные вызовы в это время получили бы `ErrOpen`: если сосед всё ещё лежит, проверять его толпой незачем.",
      scene: frame({ shown: { A: 7, B: 1, C: 1 }, cRes: "wait", result: { A: "итог: ErrOpen за 0,7 с", B: "итог: ErrOpen сразу, без сети" }, br: "half", failures: 3, edge: "timeout", up: true, calls: 4, fast: 2, hl: "C:0" }),
    },
    {
      lines: [8, 9, 10],
      note: "Проба прошла — breaker замыкается, счётчик неудач обнулён. Упади проба, breaker снова открылся бы на секунду (стрелка «проба упала»).",
      scene: frame({ shown: { A: 7, B: 1, C: 1 }, result: { A: "итог: ErrOpen за 0,7 с", B: "итог: ErrOpen сразу, без сети", C: "итог: успех" }, br: "closed", failures: 0, edge: "probe-ok", up: true, calls: 4, fast: 2, hl: "C:0" }),
    },
    {
      lines: [30, 31, 5, 8],
      note: "Склад снова сбоит. Запрос D пришёл с дедлайном 250 мс (черта «дедлайн»). Первая попытка — `Unavailable`, неудач 1 из 3.",
      scene: frame({ shown: { A: 7, B: 1, C: 1, D: 1 }, deadlineD: true, result: { A: "итог: ErrOpen за 0,7 с", B: "итог: ErrOpen сразу, без сети", C: "итог: успех" }, br: "closed", failures: 1, up: false, calls: 5, fast: 2, hl: "D:0" }),
    },
    {
      lines: [12, 13, 17, 8],
      note: "Пауза 100 мс укладывается в дедлайн, вторая попытка — снова отказ: 2 из 3, breaker остаётся замкнутым.",
      scene: frame({ shown: { A: 7, B: 1, C: 1, D: 3 }, deadlineD: true, result: { A: "итог: ErrOpen за 0,7 с", B: "итог: ErrOpen сразу, без сети", C: "итог: успех" }, br: "closed", failures: 2, up: false, calls: 6, fast: 2, hl: "D:2" }),
    },
    {
      lines: [12, 13, 14],
      note: "Следующая пауза — 200 мс, а до дедлайна осталось 150: она закончится уже после него. Ждать бессмысленно, и `reserve` сразу возвращает последний `Unavailable`. Дедлайн, а не число попыток, решил, когда остановиться.",
      scene: frame({ shown: { A: 7, B: 1, C: 1, D: 4 }, deadlineD: true, result: { A: "итог: ErrOpen за 0,7 с", B: "итог: ErrOpen сразу, без сети", C: "итог: успех", D: "итог: Unavailable за 0,1 с" }, br: "closed", failures: 2, up: false, calls: 6, fast: 2, hl: "D:3" }),
    },
  ],
});
