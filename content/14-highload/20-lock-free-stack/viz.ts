// Сценарий «Под капотом»: две горутины одновременно делают Push в стек
// Трайбера — одна выигрывает CAS, другая перечитывает вершину и повторяет.
// Потом — ABA: что было бы, если бы узлы переиспользовались, и почему в Go
// без переиспользования этого не случается (узел жив, пока на него есть ссылка).
import { arrow, box, defineScenario, goroutine, label, scene, struct } from "../../../viz/src/index.ts";
import type { GoroutineState, Shape } from "../../../viz/src/index.ts";

const code = `func (s *Stack) Push(v int) {
	n := &node{v: v}
	for {
		old := s.head.Load()
		n.next = old
		if s.head.CompareAndSwap(old, n) {
			return
		}
	}
}

func (s *Stack) Pop() (int, bool) {
	for {
		old := s.head.Load()
		if old == nil {
			return 0, false
		}
		if s.head.CompareAndSwap(old, old.next) {
			return old.v, true
		}
	}
}`;

interface Node {
  v: number;
  next: string | null;
  /** Оформление: обычный, «выброшен», «переиспользован». */
  cls?: string;
  /** Подпись под узлом вне стека. */
  note?: string;
}

interface G {
  state: GoroutineState;
  detail: string;
}

interface Frame {
  nodes: Record<string, Node>;
  /** Узлы стека от вершины вниз. */
  chain: string[];
  /** Узлы вне стека: имя → колонка (0 — под g1, 1 — под g2). */
  off?: Record<string, number>;
  g1: G;
  g2: G;
  cas?: string;
  hl?: string[];
}

const NODE_W = 112;
const CHAIN_Y = 120;
const chainX = (i: number) => 270 + i * 150;
const OFF_Y = 250;
const offX = (col: number) => 270 + col * 220;
const HEAD = { x: 10, y: 120, w: 200, h: 78 };

function frame(f: Frame): Shape[] {
  const parts: Parameters<typeof scene> = [];
  parts.push(goroutine("g1", { x: 10, y: 14, w: 330, name: "g1", state: f.g1.state, detail: f.g1.detail, hl: f.hl?.includes("g1") }));
  parts.push(goroutine("g2", { x: 370, y: 14, w: 340, name: "g2", state: f.g2.state, detail: f.g2.detail, hl: f.hl?.includes("g2") }));

  parts.push(box("head", { ...HEAD, title: "s.head", lines: f.cas ? [f.cas] : [], cls: f.hl?.includes("head") ? "v-hl" : undefined }));

  const pos: Record<string, { x: number; y: number }> = {};
  f.chain.forEach((n, i) => (pos[n] = { x: chainX(i), y: CHAIN_Y }));
  for (const [n, col] of Object.entries(f.off ?? {})) pos[n] = { x: offX(col), y: OFF_Y };

  for (const [name, p] of Object.entries(pos)) {
    const node = f.nodes[name];
    const hl = f.hl?.includes(name);
    parts.push(struct(`node:${name}`, { x: p.x, y: p.y, w: NODE_W, title: `узел ${name}`, fields: [["v", node.v], ["next", node.next ?? "nil"]], cls: [node.cls, hl && "v-hl"].filter(Boolean).join(" ") || undefined }));
    if (node.note) parts.push(label(`note:${name}`, p.x, p.y + 96, node.note, "v-caption"));
  }

  // Указатели: head → вершина, next → следующий узел.
  if (f.chain.length) {
    const top = pos[f.chain[0]];
    parts.push(arrow("p:head", { x: HEAD.x + HEAD.w, y: HEAD.y + 39 }, { x: top.x, y: top.y + 13 }, { enter: "left" }));
  }
  for (const [name, p] of Object.entries(pos)) {
    const next = f.nodes[name].next;
    if (!next || !pos[next]) continue;
    const t = pos[next];
    const from = { x: p.x + NODE_W, y: p.y + 65 };
    if (t.y === p.y && t.x < p.x) continue; // оба вне стека: связь видна по полю next
    if (t.y === p.y) parts.push(arrow(`p:${name}`, from, { x: t.x, y: t.y + 65 }, { enter: "left" }));
    else parts.push(arrow(`p:${name}`, { x: p.x + NODE_W / 2, y: p.y }, { x: t.x + NODE_W / 2, y: t.y + 78 }, { exit: "top", enter: "bottom", cls: "v-ptr v-wake" }));
  }
  return scene(...parts);
}

const run = (detail: string): G => ({ state: "running", detail });
const idle = (detail: string): G => ({ state: "runnable", detail });
const done = (detail: string): G => ({ state: "done", detail });

const A: Node = { v: 1, next: null };

export default defineScenario({
  title: "Lock-free стек на CAS",
  code,
  width: 720,
  height: 360,
  steps: [
    {
      lines: [1, 2],
      note: "В стеке один узел `A`, `s.head` указывает на него. Горутины `g1` и `g2` одновременно вызывают `Push(2)` и `Push(3)` и создают свои узлы `X` и `Y`. Узлы пока видят только их создатели.",
      scene: frame({ nodes: { A, X: { v: 2, next: null }, Y: { v: 3, next: null } }, chain: ["A"], off: { X: 0, Y: 1 }, g1: run("Push(2): n = X"), g2: run("Push(3): n = Y") }),
    },
    {
      lines: [4, 5],
      note: "Обе горутины читают вершину — это `A` — и связывают свой узел с ней: `X.next = A`, `Y.next = A`. Писать в `next` можно без атомиков: узел ещё не опубликован.",
      scene: frame({ nodes: { A, X: { v: 2, next: "A" }, Y: { v: 3, next: "A" } }, chain: ["A"], off: { X: 0, Y: 1 }, g1: run("old = A, X.next = A"), g2: run("old = A, Y.next = A"), hl: ["X", "Y"] }),
    },
    {
      lines: [6, 7],
      note: "`g1` первой выполняет `CompareAndSwap(A, X)`: вершина всё ещё `A`, значит, CAS проходит — `X` опубликован одной атомарной записью. Вместе с ним стало видно и `X.next`: запись до CAS «synchronizes before» чтение, которое увидит `X`.",
      scene: frame({ nodes: { A, X: { v: 2, next: "A" }, Y: { v: 3, next: "A" } }, chain: ["X", "A"], off: { Y: 1 }, g1: done("CAS(A, X) прошёл, Push(2) готов"), g2: run("old = A, Y.next = A"), cas: "CAS(A, X): успех", hl: ["head", "X"] }),
    },
    {
      lines: [6],
      note: "`g2` выполняет `CompareAndSwap(A, Y)` — но вершина уже `X`, не `A`. CAS не проходит и ничего не меняет: `Y` так и не увидел никто, и стек цел. Никто не ждал и не блокировался — `g2` просто проиграла гонку.",
      scene: frame({ nodes: { A, X: { v: 2, next: "A" }, Y: { v: 3, next: "A" } }, chain: ["X", "A"], off: { Y: 1 }, g1: done("Push(2) готов"), g2: run("CAS(A, Y) не прошёл"), cas: "CAS(A, Y): вершина не A", hl: ["head", "g2"] }),
    },
    {
      lines: [3, 4, 5],
      note: "Новый виток цикла: `g2` перечитывает вершину (`X`) и перепривязывает свой узел: `Y.next = X`. Работа неудачной попытки выброшена — при сильной конкуренции таких повторов много, это и есть цена lock-free.",
      scene: frame({ nodes: { A, X: { v: 2, next: "A" }, Y: { v: 3, next: "X" } }, chain: ["X", "A"], off: { Y: 1 }, g1: done("Push(2) готов"), g2: run("повтор: old = X, Y.next = X"), hl: ["Y"] }),
    },
    {
      lines: [6, 7],
      note: "`CompareAndSwap(X, Y)` проходит. В стеке `Y → X → A`: оба значения на месте, ни одно не потеряно. Это свойство lock-free: в любой момент хотя бы одна горутина продвигается, даже если другую вытеснили посреди операции.",
      scene: frame({ nodes: { A, X: { v: 2, next: "A" }, Y: { v: 3, next: "X" } }, chain: ["Y", "X", "A"], g1: done("Push(2) готов"), g2: done("CAS(X, Y) прошёл, Push(3) готов"), cas: "CAS(X, Y): успех", hl: ["head", "Y"] }),
    },
    {
      lines: [14, 18],
      note: "Теперь ABA. `g1` начинает `Pop`: читает `old = Y` и вычисляет `old.next = X` — аргумент будущего CAS. И тут её вытесняют, не дав выполнить `CompareAndSwap(Y, X)`.",
      scene: frame({ nodes: { A, X: { v: 2, next: "A" }, Y: { v: 3, next: "X" } }, chain: ["Y", "X", "A"], g1: idle("Pop: old = Y, old.next = X — вытеснена"), g2: run("работает"), hl: ["g1", "Y"] }),
    },
    {
      lines: [14, 18, 19],
      note: "Допустим, узлы переиспользуются, как в C со своим списком свободных узлов (или в Go через `sync.Pool`). `g2` выталкивает `Y` и `X`, а потом `Push(4)` берёт из списка свободных тот же узел `Y`: адрес прежний, значение новое. Вершина снова `Y` — A, B, A.",
      scene: frame({ nodes: { A, X: { v: 2, next: "A", cls: "v-muted-box", note: "вытолкнут, свободен" }, Y: { v: 4, next: "A", note: "тот же адрес, v = 4" } }, chain: ["Y", "A"], off: { X: 0 }, g1: idle("Pop: old = Y, old.next = X — вытеснена"), g2: done("Pop → 3, Pop → 2, Push(4) в узел Y"), hl: ["Y"] }),
    },
    {
      lines: [18],
      note: "`g1` просыпается и выполняет `CompareAndSwap(Y, X)`. CAS сравнивает только адрес — а вершина и правда `Y`. CAS проходит, и вершиной становится давно вытолкнутый `X`: значение 4 потеряно, 2 вытолкнут повторно. Стек испорчен.",
      scene: frame({ nodes: { A, X: { v: 2, next: "A", cls: "v-waiting" }, Y: { v: 4, next: "A", cls: "v-muted-box", note: "потерян вместе с 4" } }, chain: ["X", "A"], off: { Y: 1 }, g1: done("CAS(Y, X) прошёл — ошибка ABA"), g2: done("Push(4) готов"), cas: "CAS(Y, X): успех?!", hl: ["head", "X"] }),
    },
    {
      lines: [2, 14, 18],
      note: "В Go без переиспользования этого не случится. Пока у `g1` есть указатель `old = Y`, сборщик мусора не освободит `Y`, и `Push(4)` получит от `&node{}` новый узел `Z` с другим адресом. Вершина — `Z`, не `Y`.",
      scene: frame({ nodes: { A, Y: { v: 3, next: "X", note: "жив: на него указывает old" }, Z: { v: 4, next: "A" }, X: { v: 2, next: "A", cls: "v-muted-box" } }, chain: ["Z", "A"], off: { Y: 1, X: 0 }, g1: idle("Pop: old = Y, old.next = X — вытеснена"), g2: done("Pop → 3, Pop → 2, Push(4) в новый Z"), hl: ["Z"] }),
    },
    {
      lines: [18, 13, 14],
      note: "`CompareAndSwap(Y, X)` не проходит: вершина — `Z`. `g1` перечитывает `old = Z`, `old.next = A` и пробует снова. Стек цел. Указатель в Go — ссылка, которая держит объект живым, а не просто число-адрес, — поэтому ABA на указателях не возникает, пока вы сами не начнёте переиспользовать узлы.",
      scene: frame({ nodes: { A, Z: { v: 4, next: "A" } }, chain: ["Z", "A"], g1: run("CAS(Y, X) не прошёл, повтор: old = Z"), g2: done("Push(4) готов"), cas: "CAS(Y, X): вершина не Y", hl: ["head", "g1"] }),
    },
    {
      lines: [18, 19],
      note: "`CompareAndSwap(Z, A)` проходит, `Pop` возвращает 4. Все значения вытолкнуты ровно по разу: 3, 2, 4 — а `Y` и `X` соберёт GC, как только на них не останется ссылок.",
      scene: frame({ nodes: { A }, chain: ["A"], g1: done("Pop → 4"), g2: done("Push(4) готов"), cas: "CAS(Z, A): успех", hl: ["head"] }),
    },
  ],
});
