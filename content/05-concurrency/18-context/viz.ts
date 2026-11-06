// Сценарий «Под капотом»: дерево контекстов и распространение отмены.
import { arrow, box, defineScenario, label, scene } from "../../../viz/src/index.ts";

const code = `root := context.Background()
ctx, cancel := context.WithCancel(root)
a, cancelA := context.WithTimeout(ctx, time.Second)
d, cancelD := context.WithTimeout(a, time.Hour)
b := context.WithValue(ctx, userKey, "ann")
c, cancelC := context.WithCancel(b)
defer cancelD()
defer cancelC()

cancelA()
fmt.Println(ctx.Err(), d.Err(), c.Err())
// <nil> context canceled <nil>
cancel()
fmt.Println(b.Err(), c.Err(), c.Value(userKey))
// context canceled context canceled ann`;

type Name = "root" | "ctx" | "a" | "d" | "b" | "c";

interface Node {
  lines: string[];
  /** hl — изменился на этом шаге, canceled — отменён. */
  state?: "hl" | "canceled";
}

type Frame = Partial<Record<Name, Node>> & { out?: string[] };

const W = 190;
const H = 84;
const geom: Record<Name, { x: number; y: number; title: string; parent?: Name }> = {
  root: { x: 225, y: 26, title: "root" },
  ctx: { x: 225, y: 136, title: "ctx", parent: "root" },
  a: { x: 15, y: 246, title: "a", parent: "ctx" },
  b: { x: 435, y: 246, title: "b", parent: "ctx" },
  d: { x: 15, y: 356, title: "d", parent: "a" },
  c: { x: 435, y: 356, title: "c", parent: "b" },
};

const clsFor = (s?: Node["state"]) => (s === "hl" ? "v-hl" : s === "canceled" ? "v-waiting" : undefined);

function frame(f: Frame) {
  const parts: Parameters<typeof scene> = [label("legend", 15, 16, "стрелка — поле Context: ссылка потомка на родителя", "v-caption")];
  for (const name of Object.keys(geom) as Name[]) {
    const n = f[name];
    if (!n) continue;
    const g = geom[name];
    parts.push(box(`node:${name}`, { x: g.x, y: g.y, w: W, h: H, title: g.title, lines: n.lines, cls: clsFor(n.state) }));
    if (g.parent) {
      const p = geom[g.parent];
      const from = { x: g.x + W / 2, y: g.y };
      const to = { x: p.x + W / 2, y: p.y + H };
      parts.push(arrow(`up:${name}`, from, to, { exit: "top", enter: "bottom" }));
    }
  }
  const out = f.out ?? [];
  parts.push(box("out", { x: 15, y: 456, w: 610, h: 66, title: "вывод", lines: out }));
  return scene(...parts);
}

const open = "done: открыт";
const closed = "закрыт:";

const root: Node = { lines: ["emptyCtx", "Done() = nil", "не отменяется"] };
const ctx0: Node = { lines: ["cancelCtx", open, "children: {}"] };
const ctxA: Node = { lines: ["cancelCtx", open, "children: {a}"] };
const ctxAC: Node = { lines: ["cancelCtx", open, "children: {a, c}"] };
const ctxC: Node = { lines: ["cancelCtx", open, "children: {c}"] };
const ctxX: Node = { lines: ["cancelCtx", closed + " Canceled", "children: nil"], state: "canceled" };
const a0: Node = { lines: ["timerCtx · now+1s", open, "children: {}"] };
const aD: Node = { lines: ["timerCtx · now+1s", open, "children: {d}"] };
const aX: Node = { lines: ["timerCtx · таймер стоп", closed + " Canceled", "children: nil"], state: "canceled" };
const d0: Node = { lines: ["cancelCtx, не timerCtx", open, "дедлайн от a: now+1s"] };
const dX: Node = { lines: ["cancelCtx, не timerCtx", closed + " Canceled", "дедлайн от a: now+1s"], state: "canceled" };
const b0: Node = { lines: ["valueCtx", `user = "ann"`, "Done() — канал ctx"] };
const bX: Node = { lines: ["valueCtx", `user = "ann"`, "Done() — канал ctx"], state: "canceled" };
const c0: Node = { lines: ["cancelCtx", open, "в children у ctx"] };
const cX: Node = { lines: ["cancelCtx", closed + " Canceled", "в children у ctx"], state: "canceled" };

const out1 = "<nil> context canceled <nil>";
const out2 = "context canceled context canceled ann";

export default defineScenario({
  title: "Дерево контекстов и отмена",
  code,
  width: 640,
  height: 532,
  steps: [
    {
      lines: [1],
      note: "`Background()` — корень дерева. Это пустой контекст: `Done()` возвращает `nil`, дедлайна и значений нет, отменить его нельзя.",
      scene: frame({ root: { ...root, state: "hl" } }),
    },
    {
      lines: [2],
      note: "`WithCancel` создаёт `cancelCtx` с полем `Context` = родитель. `propagateCancel` видит, что `root.Done()` равен `nil` — родитель никогда не отменится, регистрироваться негде.",
      scene: frame({ root, ctx: { ...ctx0, state: "hl" } }),
    },
    {
      lines: [3],
      note: "`WithTimeout` — это `WithDeadline(now+1s)`: `timerCtx` с таймером. `propagateCancel` добавляет `a` в карту `children` родителя — так `ctx` узнает, кого отменять.",
      scene: frame({ root, ctx: { ...ctxA, state: "hl" }, a: { ...a0, state: "hl" } }),
    },
    {
      lines: [4],
      note: "Дедлайн потомка не может быть позже родительского. `WithDeadline` видит, что у `a` срок раньше часа, и возвращает просто `WithCancel(a)`: `d.Deadline()` — это дедлайн `a`.",
      scene: frame({ root, ctx: ctxA, a: { ...aD, state: "hl" }, d: { ...d0, state: "hl" } }),
    },
    {
      lines: [5],
      note: "`WithValue` — узел с одной парой ключ–значение. Своего канала `done` у него нет: `b.Done()` и `b.Err()` проходят к родителю `ctx`. В `children` он не попадает — отменять в нём нечего.",
      scene: frame({ root, ctx: ctxA, a: aD, d: d0, b: { ...b0, state: "hl" } }),
    },
    {
      lines: [6],
      note: "Родитель `c` — это `b`, но `propagateCancel` ищет ближайший `cancelCtx` вверх по цепочке (через `Value` со служебным ключом) и регистрирует `c` прямо в `children` у `ctx`.",
      scene: frame({ root, ctx: { ...ctxAC, state: "hl" }, a: aD, d: d0, b: b0, c: { ...c0, state: "hl" } }),
    },
    {
      lines: [10],
      note: "`cancelA()`: `a` закрывает свой `done`, останавливает таймер, отменяет всех из своих `children` — это `d` — и удаляет себя из `children` родителя. `ctx`, `b` и `c` не затронуты.",
      scene: frame({ root, ctx: { ...ctxC, state: "hl" }, a: aX, d: dX, b: b0, c: c0 }),
    },
    {
      lines: [11, 12],
      note: "Отмена идёт только вниз: `ctx` и соседняя ветка живы (`<nil>`), а `d` отменён вместе с `a`. Ошибка — `Canceled`, а не `DeadlineExceeded`: сработал `cancelA`, а не таймер.",
      scene: frame({ root, ctx: ctxC, a: aX, d: dX, b: b0, c: c0, out: [out1] }),
    },
    {
      lines: [13],
      note: "`cancel()`: `ctx` закрывает `done` и проходит по `children` — отменяется `c`. `b` отдельно отменять не нужно: его `Done()` и есть канал `ctx`. Карта `children` обнуляется.",
      scene: frame({ root, ctx: ctxX, a: aX, d: dX, b: bX, c: cX, out: [out1] }),
    },
    {
      lines: [14, 15],
      note: "Отменено всё дерево под `ctx`, `root` по-прежнему жив. Значения отмена не трогает: `c.Value(userKey)` идёт вверх по полям `Context` — `c` → `b` — и находит `\"ann\"`.",
      scene: frame({ root, ctx: ctxX, a: aX, d: dX, b: bX, c: cX, out: [out1, out2] }),
    },
  ],
});
