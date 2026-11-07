// Сценарий «Под капотом»: путь запроса через http.Server, цепочку middleware
// и ServeMux, обратный путь ответа и отмена контекста при обрыве клиента.
import { arrow, box, defineScenario, goroutine, label, scene, struct } from "../../../viz/src/index.ts";
import type { GoroutineState } from "../../../viz/src/primitives.ts";

const code = `getOrder := func(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	select {
	case o := <-load(r.Context(), id):
		writeJSON(w, http.StatusOK, o)
	case <-r.Context().Done():
		return // клиент ушёл — отвечать некому
	}
}
mux := http.NewServeMux()
mux.HandleFunc("GET /orders/{id}", getOrder)
h := logging(recoverer(requestID(mux)))
srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
go srv.Serve(ln)`;

type Layer = "server" | "logging" | "recover" | "reqid" | "mux" | "handler";

/** active — выполняется сейчас, waiting — вызвал next и ждёт возврата. */
type LayerState = { lines: string[]; state?: "active" | "waiting" };

interface Frame {
  client: string[];
  conn: { state: GoroutineState; detail: string };
  /** Горутина фонового чтения соединения — появляется, когда тело запроса прочитано. */
  reader?: { state: GoroutineState; detail: string };
  layers: Partial<Record<Layer, LayerState>>;
  req?: { ctx: string; pattern: string; id: string; hl?: string[] };
  copy?: { ctx: string; pattern: string; id: string; hl?: string[] };
  w?: { status: string; written: string; hl?: string[] };
  /** Стрелка запроса от клиента к серверу и ответа обратно. */
  wire?: "request" | "response" | "broken";
  log?: string[];
}

const PX = 200;
const PW = 190;
const geom: Record<Layer, { y: number; h: number; title: string }> = {
  server: { y: 20, h: 56, title: "http.Server" },
  logging: { y: 98, h: 56, title: "logging" },
  recover: { y: 176, h: 56, title: "recoverer" },
  reqid: { y: 254, h: 56, title: "requestID" },
  mux: { y: 332, h: 56, title: "ServeMux" },
  handler: { y: 410, h: 56, title: "getOrder" },
};
const order: Layer[] = ["server", "logging", "recover", "reqid", "mux", "handler"];
const RX = 405;
const RW = 220;

function frame(f: Frame) {
  const parts: Parameters<typeof scene> = [
    label("legend", PX, 12, "сверху вниз — вызов next.ServeHTTP, ответ идёт обратно", "v-caption"),
    box("client", { x: 15, y: 20, w: 155, h: 74, title: "клиент", lines: f.client }),
    goroutine("conn", { x: 15, y: 120, w: 155, name: "go c.serve()", state: f.conn.state, detail: f.conn.detail }),
  ];
  if (f.reader) {
    parts.push(goroutine("reader", { x: 15, y: 170, w: 155, name: "backgroundRead", state: f.reader.state, detail: f.reader.detail }));
  }
  order.forEach((name, i) => {
    const g = geom[name];
    const l = f.layers[name] ?? { lines: [] };
    const cls = l.state === "active" ? "v-hl" : l.state === "waiting" ? "v-waiting" : undefined;
    parts.push(box(`layer:${name}`, { x: PX, y: g.y, w: PW, h: g.h, title: g.title, lines: l.lines, cls }));
    if (i > 0) {
      const p = geom[order[i - 1]];
      parts.push(arrow(`call:${name}`, { x: PX + PW / 2, y: p.y + p.h }, { x: PX + PW / 2, y: g.y }, { exit: "bottom", enter: "top" }));
    }
  });
  const server = { x: PX, y: geom.server.y + 28 };
  const client = { x: 170, y: 48 };
  if (f.wire === "request") parts.push(arrow("wire", client, server, { exit: "right", enter: "left" }));
  if (f.wire === "response") parts.push(arrow("wire", server, client, { exit: "left", enter: "right" }));
  if (f.wire === "broken") parts.push(label("wire-broken", 92, 110, "✕ соединение закрыто", "v-wait", "middle"));
  if (f.req) {
    parts.push(
      struct("req", {
        x: RX,
        y: 20,
        w: RW,
        title: "r *http.Request",
        fields: [
          ["ctx", f.req.ctx],
          ["Pattern", f.req.pattern],
          ["PathValue(id)", f.req.id],
        ],
        hl: f.req.hl,
      }),
    );
  }
  if (f.copy) {
    parts.push(
      struct("copy", {
        x: RX,
        y: 150,
        w: RW,
        title: "r.WithContext(…)",
        fields: [
          ["ctx", f.copy.ctx],
          ["Pattern", f.copy.pattern],
          ["PathValue(id)", f.copy.id],
        ],
        hl: f.copy.hl,
      }),
    );
  }
  if (f.w) {
    parts.push(
      struct("w", {
        x: RX,
        y: 280,
        w: RW,
        title: "w: statusRecorder",
        fields: [
          ["status", f.w.status],
          ["записано", f.w.written],
        ],
        hl: f.w.hl,
      }),
    );
  }
  parts.push(box("log", { x: 15, y: 486, w: 610, h: 68, title: "лог сервера", lines: f.log ?? [] }));
  return scene(...parts);
}

const idle: Frame["layers"] = {
  server: { lines: ["Handler: logging(…)"] },
  logging: { lines: ["оборачивает w"] },
  recover: { lines: ["defer recover()"] },
  reqid: { lines: ["кладёт id в ctx"] },
  mux: { lines: ["GET /orders/{id}"] },
  handler: { lines: ["ждёт load или отмены"] },
};
const on = (layers: Frame["layers"], name: Layer, lines: string[], state: LayerState["state"] | null = "active") => ({
  ...layers,
  [name]: { lines, state: state ?? undefined },
});

const reqCtx = "cancelCtx";
const idCtx = "valueCtx → req";
const okLog = "GET /orders/42 → 200, req-id=7f3a";

// Первый запрос: проход вниз до хендлера.
const l1 = on(idle, "server", ["читает заголовки (5 с)"]);
const l2 = on(on(l1, "server", ["ServeHTTP(w, r)"], "waiting"), "logging", ["w → statusRecorder"]);
const l3 = on(on(l2, "logging", ["ждёт next"], "waiting"), "recover", ["defer recover()"]);
const l4 = on(on(l3, "recover", ["ждёт next"], "waiting"), "reqid", ["r2 := r.WithContext"]);
const l5 = on(on(l4, "reqid", ["ждёт next"], "waiting"), "mux", ["совпал GET /orders/{id}"]);
const l6 = on(on(l5, "mux", ["ждёт хендлер"], "waiting"), "handler", ["select: load / Done"]);
const l7 = on(l6, "handler", ["WriteHeader(200), JSON"]);
// Обратный путь: внутренние слои вернулись, logging дописывает лог.
const unwound = (outer: string[]) =>
  on(on(on(on(on(on(idle, "server", ["ServeHTTP(w, r)"], "waiting"), "handler", ["вернулся"], null), "mux", ["вернулся"], null), "reqid", ["вернулся"], null), "recover", ["паники нет"], null), "logging", outer);
const l8 = unwound(["пишет строку лога"]);
const l9 = on(on(idle, "server", ["cancel(), finishRequest"]), "logging", ["вернулся"], null);
// Второй запрос: клиент рвёт соединение, пока хендлер ждёт load.
const c1 = on(l6, "handler", ["select: load / Done"], "waiting");
const c2 = on(l6, "handler", ["<-ctx.Done(): return"]);
const c3 = { ...unwound(["статус не записан"]), server: { lines: ["закрывает соединение"] } };

export default defineScenario({
  title: "Путь HTTP-запроса через middleware",
  code,
  width: 640,
  height: 564,
  steps: [
    {
      lines: [10, 11, 12, 13],
      note: "Сборка сервера: `ServeMux` знает маршрут `GET /orders/{id}`, а `Handler` сервера — это внешняя обёртка `logging`. Цепочка собирается один раз при старте: каждая middleware получает `next` и возвращает новый `http.Handler`.",
      scene: frame({ client: ["ещё не подключён"], conn: { state: "blocked", detail: "ещё нет" }, layers: idle }),
    },
    {
      lines: [14],
      note: "`Serve` в цикле вызывает `Accept` и на каждое соединение запускает `go c.serve(ctx)`: своя горутина на соединение. Она читает заголовки запроса; `ReadHeaderTimeout` не даёт медленному клиенту держать её вечно.",
      scene: frame({
        client: ["GET /orders/42"],
        conn: { state: "running", detail: "readRequest" },
        layers: l1,
        wire: "request",
      }),
    },
    {
      lines: [12],
      note: "Запрос прочитан: `c.serve` создаёт `*http.Request` со своим контекстом — `cancelCtx`, потомком контекста соединения — и вызывает `srv.Handler.ServeHTTP` — то есть `logging`. Она подменяет `w` обёрткой `statusRecorder`, чтобы потом узнать статус ответа.",
      scene: frame({
        client: ["GET /orders/42", "ждёт ответ"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "blocked", detail: "ждёт байты" },
        layers: l2,
        req: { ctx: reqCtx, pattern: `""`, id: `""`, hl: ["ctx"] },
        w: { status: "—", written: "0 байт", hl: ["status"] },
      }),
    },
    {
      lines: [12],
      note: "`recoverer` ставит `defer` с `recover()` и вызывает `next`. Middleware — это обычные вложенные вызовы функций в одной горутине: пока внутренние слои работают, внешние стоят в стеке и ждут возврата.",
      scene: frame({
        client: ["GET /orders/42", "ждёт ответ"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "blocked", detail: "ждёт байты" },
        layers: l3,
        req: { ctx: reqCtx, pattern: `""`, id: `""` },
        w: { status: "—", written: "0 байт" },
      }),
    },
    {
      lines: [12],
      note: "`requestID` кладёт идентификатор в контекст. `*http.Request` неизменяем по соглашению, поэтому `r.WithContext` делает **копию** запроса с новым `ctx`, и дальше по цепочке идёт уже она. У внешних слоёв остаётся старый `r`.",
      scene: frame({
        client: ["GET /orders/42", "ждёт ответ"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "blocked", detail: "ждёт байты" },
        layers: l4,
        req: { ctx: reqCtx, pattern: `""`, id: `""` },
        copy: { ctx: idCtx, pattern: `""`, id: `""`, hl: ["ctx"] },
        w: { status: "—", written: "0 байт" },
      }),
    },
    {
      lines: [11],
      note: "`ServeMux` ищет самый специфичный шаблон для метода и пути и записывает его в запрос: `Pattern` и значения `{id}`. Пишет он в ту копию, которую получил, — поэтому внешний `logging` своё `r.Pattern` потом увидит пустым.",
      scene: frame({
        client: ["GET /orders/42", "ждёт ответ"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "blocked", detail: "ждёт байты" },
        layers: l5,
        req: { ctx: reqCtx, pattern: `""`, id: `""` },
        copy: { ctx: idCtx, pattern: "GET /orders/{id}", id: `"42"`, hl: ["Pattern", "PathValue(id)"] },
        w: { status: "—", written: "0 байт" },
      }),
    },
    {
      lines: [1, 2, 3, 4],
      note: "Хендлер берёт `id` из пути и ждёт в `select` одно из двух: результат `load` или отмену контекста запроса. Контекст копии — потомок контекста соединения, так что отмена сверху дойдёт и сюда.",
      scene: frame({
        client: ["GET /orders/42", "ждёт ответ"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "blocked", detail: "ждёт байты" },
        layers: l6,
        req: { ctx: reqCtx, pattern: `""`, id: `""` },
        copy: { ctx: idCtx, pattern: "GET /orders/{id}", id: `"42"`, hl: ["PathValue(id)"] },
        w: { status: "—", written: "0 байт" },
      }),
    },
    {
      lines: [4, 5],
      note: "`load` ответил — хендлер пишет ответ. `w.WriteHeader(200)` сначала попадает в `statusRecorder`: он запоминает статус и передаёт вызов настоящему `ResponseWriter`. Заголовки и тело копятся в буфере и уходят клиенту, когда он заполнится или хендлер вернётся.",
      scene: frame({
        client: ["GET /orders/42", "получает 200"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "blocked", detail: "ждёт байты" },
        layers: l7,
        req: { ctx: reqCtx, pattern: `""`, id: `""` },
        copy: { ctx: idCtx, pattern: "GET /orders/{id}", id: `"42"` },
        w: { status: "200", written: "57 байт", hl: ["status", "записано"] },
        wire: "response",
      }),
    },
    {
      lines: [12],
      note: "Обратный путь — это возвраты из вложенных вызовов в обратном порядке: `mux`, `requestID`, `recoverer` (паники не было, `recover()` вернул `nil`) и наконец `logging`, который читает статус из обёртки и пишет строку лога.",
      scene: frame({
        client: ["GET /orders/42", "получил 200"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "blocked", detail: "ждёт байты" },
        layers: l8,
        req: { ctx: reqCtx, pattern: `""`, id: `""` },
        w: { status: "200", written: "57 байт", hl: ["status"] },
        log: [okLog],
      }),
    },
    {
      lines: [14],
      note: "`ServeHTTP` вернулся — `c.serve` отменяет контекст запроса и дописывает ответ из буфера: `r.Context()` живёт ровно столько, сколько обработка. Фоновую работу, которая должна пережить ответ, запускают с `context.WithoutCancel`.",
      scene: frame({
        client: ["keep-alive", "может слать следующий"],
        conn: { state: "running", detail: "finishRequest" },
        layers: l9,
        req: { ctx: "отменён: Canceled", pattern: `""`, id: `""`, hl: ["ctx"] },
        w: { status: "200", written: "57 байт" },
        log: [okLog],
      }),
    },
    {
      lines: [3, 4, 6],
      note: "Второй запрос, `load` отвечает долго, клиент не дождался и закрыл соединение. Хендлер в это время стоит в `select`, остальные слои — в стеке над ним. Читать из соединения сейчас некому, кроме фоновой горутины.",
      scene: frame({
        client: ["GET /orders/43", "закрыл соединение"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "running", detail: "Read → EOF" },
        layers: c1,
        req: { ctx: reqCtx, pattern: `""`, id: `""` },
        copy: { ctx: idCtx, pattern: "GET /orders/{id}", id: `"43"` },
        w: { status: "—", written: "0 байт" },
        wire: "broken",
        log: [okLog],
      }),
    },
    {
      lines: [6, 7],
      note: "Фоновое чтение получает EOF и отменяет контекст соединения, а с ним — контексты всех копий запроса. `select` в хендлере выбирает `ctx.Done()`, хендлер выходит без ответа, `load` получает отменённый контекст и тоже бросает работу.",
      scene: frame({
        client: ["GET /orders/43", "отключён"],
        conn: { state: "running", detail: "ServeHTTP" },
        reader: { state: "done", detail: "cancelCtx()" },
        layers: c2,
        req: { ctx: "отменён: Canceled", pattern: `""`, id: `""`, hl: ["ctx"] },
        copy: { ctx: "отменён: Canceled", pattern: "GET /orders/{id}", id: `"43"`, hl: ["ctx"] },
        w: { status: "—", written: "0 байт" },
        wire: "broken",
        log: [okLog],
      }),
    },
    {
      lines: [12],
      note: "Стек разматывается как обычно, и `logging` видит в обёртке: статус не записан, `r.Context().Err()` — `context.Canceled`. Такой запрос стоит логировать отдельно («клиент ушёл»), а не как успешный 200.",
      scene: frame({
        client: ["GET /orders/43", "отключён"],
        conn: { state: "done", detail: "соединение закрыто" },
        layers: c3,
        req: { ctx: "отменён: Canceled", pattern: `""`, id: `""` },
        w: { status: "—", written: "0 байт", hl: ["status"] },
        wire: "broken",
        log: [okLog, "GET /orders/43 → клиент ушёл: context canceled"],
      }),
    },
  ],
});
