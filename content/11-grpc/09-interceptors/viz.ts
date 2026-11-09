// Сценарий «Под капотом»: путь unary-вызова gRPC — клиентский interceptor,
// кадры HTTP/2 (заголовки с metadata, DATA с сообщением, трейлеры со
// статусом), серверная цепочка logging → recovery → auth, обработчик и
// обратный путь; затем вызов с паникой и ответ trailers-only.
import { arrow, box, defineScenario, label, scene, struct } from "../../../viz/src/index.ts";

const code = `srv := grpc.NewServer(grpc.ChainUnaryInterceptor(logging, recovery, auth))
RegisterOrderServiceServer(srv, orders{})
conn, _ := grpc.NewClient(target, creds,
	grpc.WithChainUnaryInterceptor(withRequestID()))
client := NewOrderServiceClient(conn)
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()
ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer s3cret")
resp, err := client.GetOrder(ctx, &GetOrderRequest{Id: 1})
fmt.Println(resp.GetStatus(), status.Code(err))
resp, err = client.GetOrder(ctx, &GetOrderRequest{Id: 13})
fmt.Println(resp.GetStatus(), status.Code(err))`;

type Side = "app" | "reqid" | "ctransport";
type Layer = "stransport" | "logging" | "recovery" | "auth" | "handler";
/** active — выполняется сейчас, waiting — вызвал следующее звено и ждёт. */
type State = { lines: string[]; state?: "active" | "waiting" | "error" };
type Frame = { title: string; lines: string[]; hl?: boolean; bad?: boolean };

interface Shot {
  client: Partial<Record<Side, State>>;
  server: Partial<Record<Layer, State>>;
  md?: [string, string][];
  req?: Frame[];
  resp?: Frame[];
  wire?: "out" | "in";
  log?: string[];
}

const LX = 10;
const LW = 175;
const MX = 205;
const MW = 340;
const RX = 580;
const RW = 170;

const clientGeom: Record<Side, { y: number; title: string }> = {
  app: { y: 40, title: "client.GetOrder" },
  reqid: { y: 122, title: "withRequestID" },
  ctransport: { y: 204, title: "транспорт клиента" },
};
const serverGeom: Record<Layer, { y: number; title: string }> = {
  stransport: { y: 40, title: "транспорт сервера" },
  logging: { y: 122, title: "logging" },
  recovery: { y: 204, title: "recovery" },
  auth: { y: 286, title: "auth" },
  handler: { y: 368, title: "GetOrder" },
};
const BOX_H = 66;
const clientOrder: Side[] = ["app", "reqid", "ctransport"];
const serverOrder: Layer[] = ["stransport", "logging", "recovery", "auth", "handler"];

const cls = (s?: State) => (s?.state === "active" ? "v-hl" : s?.state === "waiting" ? "v-waiting" : s?.state === "error" ? "v-bad" : undefined);
const frameH = (f: Frame) => 30 + f.lines.length * 18;

function shot(s: Shot) {
  const parts: Parameters<typeof scene> = [
    label("h:client", LX, 26, "клиент", "v-caption"),
    label("h:wire", MX, 26, "HTTP/2: один поток на вызов", "v-caption"),
    label("h:server", RX, 26, "сервер", "v-caption"),
  ];
  clientOrder.forEach((name, i) => {
    const g = clientGeom[name];
    const st = s.client[name];
    parts.push(box(`c:${name}`, { x: LX, y: g.y, w: LW, h: BOX_H, title: g.title, lines: st?.lines ?? [], cls: cls(st) }));
    if (i > 0) {
      const p = clientGeom[clientOrder[i - 1]];
      parts.push(arrow(`c:call:${name}`, { x: LX + LW / 2, y: p.y + BOX_H }, { x: LX + LW / 2, y: g.y }, { exit: "bottom", enter: "top" }));
    }
  });
  serverOrder.forEach((name, i) => {
    const g = serverGeom[name];
    const st = s.server[name];
    parts.push(box(`s:${name}`, { x: RX, y: g.y, w: RW, h: BOX_H, title: g.title, lines: st?.lines ?? [], cls: cls(st) }));
    if (i > 0) {
      const p = serverGeom[serverOrder[i - 1]];
      parts.push(arrow(`s:call:${name}`, { x: RX + RW / 2, y: p.y + BOX_H }, { x: RX + RW / 2, y: g.y }, { exit: "bottom", enter: "top" }));
    }
  });
  if (s.md) parts.push(struct("md", { x: LX, y: 292, w: LW, title: "исходящий ctx", fields: s.md }));

  // Кадры: запрос сверху, ответ под ним.
  let y = 40;
  const mids: Record<string, number[]> = { req: [], resp: [] };
  const frames = (key: string, list: Frame[] | undefined) => {
    (list ?? []).forEach((f, i) => {
      const c = f.bad ? "v-bad" : f.hl ? "v-hl" : undefined;
      parts.push(box(`${key}:${i}`, { x: MX, y, w: MW, h: frameH(f), title: f.title, lines: f.lines, cls: c }));
      mids[key].push(y + frameH(f) / 2);
      y += frameH(f) + 8;
    });
  };
  frames("req", s.req);
  if (s.resp?.length) {
    parts.push(label("h:resp", MX, y + 10, "ответ ↓", "v-caption"));
    y += 18;
  }
  frames("resp", s.resp);

  const ct = { x: LX + LW, y: clientGeom.ctransport.y + BOX_H / 2 };
  const st = { x: RX, y: serverGeom.stransport.y + BOX_H / 2 };
  // Кадры уходят от транспорта клиента к серверу и обратно — стрелки
  // идут через последний кадр, а не поверх колонки.
  const last = (key: string) => ({ y: mids[key][mids[key].length - 1] });
  if (s.wire === "out" && mids.req.length) {
    parts.push(arrow("wire:a", ct, { x: MX, ...last("req") }, { exit: "right", enter: "left" }));
    parts.push(arrow("wire:b", { x: MX + MW, ...last("req") }, st, { exit: "right", enter: "left" }));
  }
  if (s.wire === "in" && mids.resp.length) {
    parts.push(arrow("wire:a", st, { x: MX + MW, y: mids.resp[0] }, { exit: "left", enter: "right" }));
    parts.push(arrow("wire:b", { x: MX, y: mids.resp[0] }, ct, { exit: "left", enter: "right" }));
  }
  parts.push(box("log", { x: LX, y: 458, w: 740, h: 86, title: "лог сервера", lines: s.log ?? [] }));
  return scene(...parts);
}

const md: [string, string][] = [
  ["дедлайн", "1 с"],
  ["authorization", "…"],
];
const mdRid: [string, string][] = [...md, ["x-request-id", "req-1"]];

const idleServer: Shot["server"] = {
  stransport: { lines: ["ждёт потоки"] },
  logging: { lines: ["внешнее звено"] },
  recovery: { lines: ["defer recover()"] },
  auth: { lines: ["проверяет токен"] },
  handler: { lines: ["обработчик"] },
};
const idleClient: Shot["client"] = {
  app: { lines: ["сгенерированный код"] },
  reqid: { lines: ["клиентское звено"] },
  ctransport: { lines: ["соединения нет"] },
};

const reqHeaders = (rid: string, hl = false): Frame => ({
  title: "HEADERS — заголовки запроса",
  lines: [":method POST", ":path /orders.v1.OrderService/GetOrder", "content-type application/grpc", "grpc-timeout 999950u", "authorization Bearer s3cret", `x-request-id ${rid}`],
  hl,
});
const reqData = (payload: string, note: string, hl = false): Frame => ({
  title: "DATA + END_STREAM — сообщение",
  lines: [`00 00000002 ${payload}`, note],
  hl,
});
const respHeaders: Frame = { title: "HEADERS — ответ", lines: [":status 200 · application/grpc"] };
const respData: Frame = { title: "DATA — GetOrderResponse", lines: ["00 00000008 08 01 12 04 70 61 69 64"] };
const trailers: Frame = { title: "HEADERS + END_STREAM — трейлеры", lines: ["grpc-status 0"] };

const logOK = "/orders.v1.OrderService/GetOrder rid=req-1 code=OK";

const S = (server: Shot["server"], patch: Shot["server"]) => ({ ...server, ...patch });

export default defineScenario({
  title: "Путь gRPC-вызова через interceptors",
  code,
  width: 760,
  height: 556,
  steps: [
    {
      lines: [1, 2, 3, 4, 5],
      note: "Сервер собирает цепочку один раз: `ChainUnaryInterceptor(logging, recovery, auth)` — первое звено внешнее. У клиента своя цепочка, `WithChainUnaryInterceptor`. `grpc.NewClient` соединение не открывает: оно появится при первом вызове.",
      scene: shot({ client: idleClient, server: idleServer }),
    },
    {
      lines: [6, 7, 8],
      note: "Всё, что уйдёт на сервер кроме самого сообщения, едет в `ctx`: дедлайн из `WithTimeout` и исходящая metadata из `AppendToOutgoingContext`. Metadata — это пары «ключ → значения»; ключи приводятся к нижнему регистру.",
      scene: shot({ client: idleClient, server: idleServer, md }),
    },
    {
      lines: [9],
      note: "Сгенерированный `GetOrder` вызывает `conn.Invoke`, а тот — клиентскую цепочку. `withRequestID` дописывает в metadata `x-request-id` и вызывает `invoker` — остаток пути: сериализацию и транспорт.",
      scene: shot({
        client: { app: { lines: ["ждёт ответ"], state: "waiting" }, reqid: { lines: ["+ x-request-id"], state: "active" }, ctransport: { lines: ["соединения нет"] } },
        server: idleServer,
        md: mdRid,
      }),
    },
    {
      lines: [9],
      note: "Транспорт открывает соединение и новый поток HTTP/2. Первый кадр — HEADERS: метод всегда `POST`, `:path` — полное имя метода, дедлайн превращается в `grpc-timeout` (остаток, здесь в микросекундах), metadata — в обычные заголовки.",
      scene: shot({
        client: { app: { lines: ["ждёт ответ"], state: "waiting" }, reqid: { lines: ["ждёт invoker"], state: "waiting" }, ctransport: { lines: ["пишет кадры"], state: "active" } },
        server: idleServer,
        md: mdRid,
        req: [reqHeaders("req-1", true)],
        wire: "out",
      }),
    },
    {
      lines: [9],
      note: "Сообщение уходит кадром DATA: 1 байт флага сжатия, 4 байта длины и байты protobuf — `08 01` значит «поле 1, varint, 1». Unary-клиент сразу ставит END_STREAM: больше он в этот поток ничего не пошлёт.",
      scene: shot({
        client: { app: { lines: ["ждёт ответ"], state: "waiting" }, reqid: { lines: ["ждёт invoker"], state: "waiting" }, ctransport: { lines: ["ждёт ответ"], state: "waiting" } },
        server: idleServer,
        md: mdRid,
        req: [reqHeaders("req-1"), reqData("08 01", "флаг · длина · id = 1", true)],
        wire: "out",
      }),
    },
    {
      lines: [1],
      note: "Сервер собирает контекст вызова: заголовки становятся входящей metadata (`metadata.FromIncomingContext`), `grpc-timeout` — дедлайном `ctx`. Затем вызывает цепочку. `logging` на входе запоминает метод и request id и вызывает `handler` — следующее звено.",
      scene: shot({
        client: { app: { lines: ["ждёт ответ"], state: "waiting" }, reqid: { lines: ["ждёт invoker"], state: "waiting" }, ctransport: { lines: ["ждёт ответ"], state: "waiting" } },
        server: S(idleServer, { stransport: { lines: ["ctx: md + дедлайн"], state: "waiting" }, logging: { lines: ["→ rid=req-1"], state: "active" } }),
        md: mdRid,
        req: [reqHeaders("req-1"), reqData("08 01", "флаг · длина · id = 1")],
      }),
    },
    {
      lines: [1],
      note: "`recovery` ставит `defer` с `recover()` и зовёт дальше. `auth` находит `authorization`, сравнивает токен за постоянное время и кладёт пользователя в `ctx`. Отказ здесь вернул бы `Unauthenticated`, и обработчик не был бы вызван вовсе.",
      scene: shot({
        client: { app: { lines: ["ждёт ответ"], state: "waiting" }, reqid: { lines: ["ждёт invoker"], state: "waiting" }, ctransport: { lines: ["ждёт ответ"], state: "waiting" } },
        server: S(idleServer, {
          stransport: { lines: ["ctx: md + дедлайн"], state: "waiting" },
          logging: { lines: ["ждёт handler"], state: "waiting" },
          recovery: { lines: ["defer recover()"], state: "waiting" },
          auth: { lines: ["токен ok → user"], state: "active" },
        }),
        md: mdRid,
        req: [reqHeaders("req-1"), reqData("08 01", "флаг · длина · id = 1")],
      }),
    },
    {
      lines: [1, 2],
      note: "Обработчик получает уже десериализованный `*GetOrderRequest` и `ctx` со всем, что положили звенья выше. Interceptors — вложенные вызовы в одной горутине: пока обработчик работает, они стоят в стеке над ним.",
      scene: shot({
        client: { app: { lines: ["ждёт ответ"], state: "waiting" }, reqid: { lines: ["ждёт invoker"], state: "waiting" }, ctransport: { lines: ["ждёт ответ"], state: "waiting" } },
        server: S(idleServer, {
          stransport: { lines: ["ctx: md + дедлайн"], state: "waiting" },
          logging: { lines: ["ждёт handler"], state: "waiting" },
          recovery: { lines: ["defer recover()"], state: "waiting" },
          auth: { lines: ["ждёт handler"], state: "waiting" },
          handler: { lines: ["id=1 → paid"], state: "active" },
        }),
        md: mdRid,
        req: [reqHeaders("req-1"), reqData("08 01", "флаг · длина · id = 1")],
      }),
    },
    {
      lines: [1],
      note: "Обратный путь — возвраты в обратном порядке: `auth`, `recovery` (паники нет) и `logging`, который пишет строку лога с итоговым кодом. Ответ ещё не отправлен: транспорт пишет его, только когда вся цепочка вернула результат.",
      scene: shot({
        client: { app: { lines: ["ждёт ответ"], state: "waiting" }, reqid: { lines: ["ждёт invoker"], state: "waiting" }, ctransport: { lines: ["ждёт ответ"], state: "waiting" } },
        server: S(idleServer, {
          stransport: { lines: ["ctx: md + дедлайн"], state: "waiting" },
          logging: { lines: ["← code=OK"], state: "active" },
          recovery: { lines: ["вернулся"] },
          auth: { lines: ["вернулся"] },
          handler: { lines: ["вернул ответ"] },
        }),
        md: mdRid,
        req: [reqHeaders("req-1"), reqData("08 01", "флаг · длина · id = 1")],
        log: [logOK],
      }),
    },
    {
      lines: [9, 10],
      note: "Ответ — три кадра: HEADERS со `:status 200` (HTTP-статус в gRPC почти всегда 200), DATA с сообщением и трейлеры — HEADERS после тела с `grpc-status`. Статус вызова живёт в трейлерах, потому что становится известен только в конце. Клиент печатает `paid OK`.",
      scene: shot({
        client: { app: { lines: ["paid OK"], state: "active" }, reqid: { lines: ["вернулся"] }, ctransport: { lines: ["поток закрыт"] } },
        server: S(idleServer, { stransport: { lines: ["пишет ответ"], state: "active" } }),
        md: mdRid,
        req: [reqHeaders("req-1"), reqData("08 01", "флаг · длина · id = 1")],
        resp: [respHeaders, respData, trailers],
        wire: "in",
        log: [logOK],
      }),
    },
    {
      lines: [11],
      note: "Второй вызов, `Id: 13`: обработчик паникует. Без `recovery` упал бы весь процесс — в grpc-go нет встроенного перехвата. `recovery` в `defer` ловит панику, пишет детали в лог и возвращает `status.Error(codes.Internal, \"internal error\")`.",
      scene: shot({
        client: { app: { lines: ["ждёт ответ"], state: "waiting" }, reqid: { lines: ["ждёт invoker"], state: "waiting" }, ctransport: { lines: ["ждёт ответ"], state: "waiting" } },
        server: S(idleServer, {
          stransport: { lines: ["ctx: md + дедлайн"], state: "waiting" },
          logging: { lines: ["ждёт handler"], state: "waiting" },
          recovery: { lines: ["recover() → Internal"], state: "active" },
          auth: { lines: ["размотан паникой"] },
          handler: { lines: ["panic: nil map"], state: "error" },
        }),
        md: [...md, ["x-request-id", "req-2"]],
        req: [reqHeaders("req-2"), reqData("08 0d", "флаг · длина · id = 13")],
        log: [logOK, "паника в GetOrder rid=req-2: assignment to entry in nil map"],
      }),
    },
    {
      lines: [11, 12],
      note: "`logging` видит `Internal`, потому что стоит снаружи `recovery`. Сообщения нет, поэтому ответ — trailers-only: один кадр HEADERS сразу с `grpc-status 13` и `grpc-message`. Клиент получает `resp == nil`, а геттер `GetStatus()` у `nil` безопасно вернёт пустую строку: печать — ` Internal`.",
      scene: shot({
        client: { app: { lines: ["\"\" Internal"], state: "error" }, reqid: { lines: ["вернулся"] }, ctransport: { lines: ["поток закрыт"] } },
        server: S(idleServer, { stransport: { lines: ["пишет статус"], state: "active" }, logging: { lines: ["← code=Internal"] } }),
        md: [...md, ["x-request-id", "req-2"]],
        req: [reqHeaders("req-2"), reqData("08 0d", "флаг · длина · id = 13")],
        resp: [{ title: "HEADERS + END_STREAM — trailers-only", lines: [":status 200 · application/grpc", "grpc-status 13", "grpc-message internal error"], bad: true }],
        wire: "in",
        log: [logOK, "паника в GetOrder rid=req-2: assignment to entry in nil map", "/orders.v1.OrderService/GetOrder rid=req-2 code=Internal"],
      }),
    },
  ],
});
