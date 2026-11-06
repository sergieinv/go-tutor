// Сценарий «Под капотом»: устройство канала (runtime.hchan) —
// кольцевой буфер, очереди ждущих горутин, прямая передача и close.
import { arrow, box, defineScenario, goroutine, label, memory, scene, struct } from "../../../viz/src/index.ts";
import type { GoroutineState } from "../../../viz/src/index.ts";

const code = `ch := make(chan int, 2)
ch <- 1
ch <- 2
go func() { ch <- 3 }() // G1
time.Sleep(time.Millisecond) // G1 успевает заблокироваться
fmt.Println(<-ch)            // 1
fmt.Println(<-ch, <-ch)      // 2 3
go func() { fmt.Println(<-ch) }() // G2
time.Sleep(time.Millisecond)
ch <- 4
for range 2 { // G3, G4
	go func() { v, ok := <-ch; fmt.Println(v, ok) }()
}
time.Sleep(time.Millisecond)
close(ch)`;

interface G {
  name: string;
  state: GoroutineState;
  detail: string;
}

interface Frame {
  qcount: number;
  buf: (number | null)[];
  sendx: number;
  recvx: number;
  closed?: boolean;
  recvq: G[];
  sendq: G[];
  /** Горутины, которые уже не в очередях, но ещё видны на месте. */
  woken?: { recv?: G[]; send?: G[] };
  main: { state: GoroutineState; detail: string };
  out: string[];
  hl?: string[];
  hlBuf?: number[];
}

const H = { x: 10, y: 70 };
const BUF = { x: 330, y: 44 };
const RECVQ = { x: 330, y: 152 };
const SENDQ = { x: 330, y: 260 };

function frame(f: Frame) {
  const qName = (q: G[]) => (q.length ? q.map((g) => g.name).join(", ") : "пусто");
  const ch = box("ch", { x: H.x, y: 10, w: 220, h: 34, title: "ch — указатель на hchan" });
  const h = struct("hchan", {
    x: H.x,
    y: H.y,
    w: 220,
    title: "runtime.hchan",
    fields: [
      ["qcount", f.qcount],
      ["dataqsiz", 2],
      ["buf", "→ [2]int"],
      ["sendx", f.sendx],
      ["recvx", f.recvx],
      ["recvq", qName(f.recvq)],
      ["sendq", qName(f.sendq)],
      ["closed", f.closed ? 1 : 0],
    ],
    hl: f.hl,
  });
  const buf = memory("buf", { x: BUF.x, y: BUF.y, values: f.buf, title: "buf — кольцевой буфер на 2 элемента", cellW: 60, hl: f.hlBuf });
  const parts: Parameters<typeof scene> = [ch, h, buf, arrow("ch:ptr", ch.at("bottom"), { x: H.x + 110, y: H.y }, { exit: "bottom", enter: "top" }), arrow("buf:ptr", h.at("buf"), buf.at(-1), { enter: "left" })];

  // Метки sendx/recvx под ячейками буфера.
  parts.push(label("sendx", BUF.x + f.sendx * 60 + 4, BUF.y + 66, "↑ sendx", "v-caption"));
  parts.push(label("recvx", BUF.x + f.recvx * 60 + 4, BUF.y + 80, "↑ recvx", "v-caption"));

  parts.push(label("recvq:title", RECVQ.x, RECVQ.y - 8, "recvq — ждут получатели (sudog)", "v-caption"));
  parts.push(label("sendq:title", SENDQ.x, SENDQ.y - 8, "sendq — ждут отправители (sudog)", "v-caption"));
  const recv = [...f.recvq, ...(f.woken?.recv ?? [])];
  const send = [...f.sendq, ...(f.woken?.send ?? [])];
  recv.forEach((g, i) => parts.push(goroutine(`g:${g.name}`, { x: RECVQ.x, y: RECVQ.y + i * 40, name: g.name, state: g.state, detail: g.detail, w: 290 })));
  send.forEach((g, i) => parts.push(goroutine(`g:${g.name}`, { x: SENDQ.x, y: SENDQ.y + i * 40, name: g.name, state: g.state, detail: g.detail, w: 290 })));
  if (f.recvq.length) parts.push(arrow("recvq:ptr", h.at("recvq"), { x: RECVQ.x, y: RECVQ.y + 17 }, { enter: "left" }));
  if (f.sendq.length) parts.push(arrow("sendq:ptr", h.at("sendq"), { x: SENDQ.x, y: SENDQ.y + 17 }, { enter: "left" }));

  parts.push(goroutine("g:main", { x: H.x, y: 318, name: "main", state: f.main.state, detail: f.main.detail, w: 290 }));
  parts.push(box("out", { x: H.x, y: 366, w: 620, h: 52, title: "вывод", lines: [f.out.join("  ·  ")] }));
  return scene(...parts);
}

const g1Blocked: G = { name: "G1", state: "blocked", detail: "ch <- 3: ждёт места, sudog.elem = 3" };

export default defineScenario({
  title: "Устройство канала: hchan, буфер и очереди ожидания",
  code,
  width: 640,
  height: 430,
  steps: [
    {
      lines: [1],
      note: "`make(chan int, 2)` выделяет в куче структуру `hchan` и буфер на 2 элемента. `ch` — лишь указатель на неё, поэтому канал можно копировать и передавать в функции: все копии смотрят на один `hchan`.",
      scene: frame({ qcount: 0, buf: [null, null], sendx: 0, recvx: 0, recvq: [], sendq: [], main: { state: "running", detail: "выполняется" }, out: [], hl: ["qcount", "dataqsiz", "buf"] }),
    },
    {
      lines: [2],
      note: "Отправка: получателей в `recvq` нет, в буфере есть место (`qcount < dataqsiz`). Значение копируется в `buf[sendx]`, `sendx` сдвигается, `qcount` растёт. Отправитель не блокируется.",
      scene: frame({ qcount: 1, buf: [1, null], sendx: 1, recvx: 0, recvq: [], sendq: [], main: { state: "running", detail: "выполняется" }, out: [], hl: ["qcount", "sendx"], hlBuf: [0] }),
    },
    {
      lines: [3],
      note: "Второе значение ложится в `buf[1]`. `sendx` дошёл до конца буфера и вернулся к 0 — буфер кольцевой. Теперь `qcount == dataqsiz`: буфер полон.",
      scene: frame({ qcount: 2, buf: [1, 2], sendx: 0, recvx: 0, recvq: [], sendq: [], main: { state: "running", detail: "выполняется" }, out: [], hl: ["qcount", "sendx"], hlBuf: [1] }),
    },
    {
      lines: [4, 5],
      note: "G1 отправляет 3, но места нет. Рантайм заводит `sudog` — запись «горутина G1 ждёт на этом канале, её значение лежит по адресу `elem`», — ставит её в `sendq` и усыпляет G1 (`gopark`). Поток при этом не блокируется.",
      scene: frame({ qcount: 2, buf: [1, 2], sendx: 0, recvx: 0, recvq: [], sendq: [g1Blocked], main: { state: "blocked", detail: "time.Sleep" }, out: [], hl: ["sendq"] }),
    },
    {
      lines: [6],
      note: "Получение при ждущем отправителе: `main` забирает голову буфера `buf[recvx]` = 1, а значение G1 копируется в освободившийся слот — в хвост очереди. Порядок FIFO сохранён: 2 выйдет раньше 3. `qcount` не меняется, G1 разбужена (`goready`).",
      scene: frame({
        qcount: 2,
        buf: [3, 2],
        sendx: 1,
        recvx: 1,
        recvq: [],
        sendq: [],
        woken: { send: [{ name: "G1", state: "runnable", detail: "разбужена: отправка состоялась" }] },
        main: { state: "running", detail: "получила 1" },
        out: ["1"],
        hl: ["recvx", "sendx", "sendq"],
        hlBuf: [0],
      }),
    },
    {
      lines: [7],
      note: "Два получения подряд читают `buf[1]` = 2 и `buf[0]` = 3 — значения выходят в порядке отправки. `qcount` = 0, буфер пуст. G1 доработала и завершилась.",
      scene: frame({ qcount: 0, buf: [null, null], sendx: 1, recvx: 1, recvq: [], sendq: [], main: { state: "running", detail: "получила 2 и 3" }, out: ["1", "2 3"], hl: ["qcount", "recvx"] }),
    },
    {
      lines: [8, 9],
      note: "G2 хочет получить, но буфер пуст и отправителей нет. Её `sudog` встаёт в `recvq`; `elem` указывает на переменную в стеке G2, куда должно попасть значение.",
      scene: frame({
        qcount: 0,
        buf: [null, null],
        sendx: 1,
        recvx: 1,
        recvq: [{ name: "G2", state: "blocked", detail: "<-ch: ждёт значение" }],
        sendq: [],
        main: { state: "blocked", detail: "time.Sleep" },
        out: ["1", "2 3"],
        hl: ["recvq"],
      }),
    },
    {
      lines: [10],
      note: "Прямая передача: раз в `recvq` есть получатель, отправка не трогает буфер — 4 копируется прямо в стек G2 (`sendDirect`), и G2 будится. `qcount`, `sendx` и `recvx` не изменились. Так же работает и небуферизованный канал: у него буфера нет вовсе.",
      scene: frame({
        qcount: 0,
        buf: [null, null],
        sendx: 1,
        recvx: 1,
        recvq: [],
        sendq: [],
        woken: { recv: [{ name: "G2", state: "runnable", detail: "получила 4 напрямую, минуя буфер" }] },
        main: { state: "running", detail: "отправила 4" },
        out: ["1", "2 3", "4"],
        hl: ["recvq"],
      }),
    },
    {
      lines: [11, 12, 13, 14],
      note: "G3 и G4 тоже ждут значения — обе в `recvq`, в порядке прихода. Очередь ожидания — связный список `sudog`, по одному на каждую ждущую горутину.",
      scene: frame({
        qcount: 0,
        buf: [null, null],
        sendx: 1,
        recvx: 1,
        recvq: [
          { name: "G3", state: "blocked", detail: "v, ok := <-ch: ждёт" },
          { name: "G4", state: "blocked", detail: "v, ok := <-ch: ждёт" },
        ],
        sendq: [],
        main: { state: "blocked", detail: "time.Sleep" },
        out: ["1", "2 3", "4"],
        hl: ["recvq"],
      }),
    },
    {
      lines: [15],
      note: "`close` ставит `closed` = 1 и будит всех из `recvq` разом: каждому достаётся нулевое значение и `ok == false`. Ждущих отправителей из `sendq` он тоже разбудил бы — и они бы запаниковали. Поэтому закрытие — широковещательный сигнал.",
      scene: frame({
        qcount: 0,
        buf: [null, null],
        sendx: 1,
        recvx: 1,
        closed: true,
        recvq: [],
        sendq: [],
        woken: {
          recv: [
            { name: "G3", state: "runnable", detail: "разбужена: v = 0, ok = false" },
            { name: "G4", state: "runnable", detail: "разбужена: v = 0, ok = false" },
          ],
        },
        main: { state: "running", detail: "закрыла канал" },
        out: ["1", "2 3", "4", "0 false", "0 false"],
        hl: ["closed", "recvq"],
      }),
    },
  ],
});
