// Сценарий «Под капотом»: пул *sql.DB с MaxOpenConns = 2. Четыре горутины
// просят соединения, двое лишних ждут в очереди, а обработчик с ранним return
// без rows.Close() не возвращает соединение — пул «тает», и следующий запрос
// дожидается только таймаута своего контекста.
import { arrow, box, defineScenario, goroutine, label, memory, scene, struct } from "../../../viz/src/index.ts";
import type { GoroutineState, Shape } from "../../../viz/src/index.ts";

const code = `db.SetMaxOpenConns(2)

handle := func(ctx context.Context, id int) error {
	rows, err := db.QueryContext(ctx, "SELECT name FROM items WHERE id >= ?", id)
	if err != nil {
		return err // в том числе таймаут ожидания соединения
	}
	if id%2 == 0 {
		return errSkip // УТЕЧКА: rows не закрыт
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

for id := 1; id <= 4; id++ {
	go handle(ctx, id)
}
time.Sleep(50 * time.Millisecond) // новый запрос приходит позже

ctx5, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
defer cancel()
err := handle(ctx5, 5) // context deadline exceeded`;

type ConnState = "none" | "busy" | "idle" | "leak";

interface Conn {
  state: ConnState;
  /** Кто держит соединение (для busy). */
  owner?: string;
}

interface G {
  state: GoroutineState;
  detail?: string;
}

interface Frame {
  conns: [Conn, Conn];
  gs: Record<string, G | undefined>;
  queue: string[];
  waitCount: number;
  /** Подпись под очередью. */
  queueNote?: string;
  hl?: string[];
}

const NAMES = ["g1", "g2", "g3", "g4", "main"];
const G_X = 10;
const G_W = 230;
const gY = (i: number) => 30 + i * 56;

const POOL = { x: 290, y: 30, w: 420, h: 170 };
const CONN = { x: 305, w: 190, h: 56 };
const connY = (i: number) => 60 + i * 66;
const Q = { x: 515, y: 96 };

const connLines: Record<ConnState, (c: Conn) => string[]> = {
  none: () => ["ещё не открыто"],
  busy: (c) => [`занято: ${c.owner}`],
  idle: () => ["простаивает (Idle)"],
  leak: () => ["утекло, не вернётся"],
};

function frame(f: Frame): Shape[] {
  const parts: Parameters<typeof scene> = [];
  parts.push(label("gcap", G_X, 18, "горутины", "v-caption"));
  NAMES.forEach((name, i) => {
    const g = f.gs[name];
    if (!g) return;
    parts.push(goroutine(`g:${name}`, { x: G_X, y: gY(i), w: G_W, name, state: g.state, detail: g.detail, hl: f.hl?.includes(name) }));
  });

  parts.push(box("pool", { ...POOL, title: "пул *sql.DB · MaxOpenConns = 2" }));
  f.conns.forEach((c, i) => {
    parts.push(
      box(`conn${i}`, {
        x: CONN.x,
        y: connY(i),
        w: CONN.w,
        h: CONN.h,
        title: `conn #${i + 1}`,
        lines: connLines[c.state](c),
        cls: c.state === "none" ? "v-cell-empty" : c.state === "leak" ? "v-hl v-waiting" : c.state === "idle" ? "v-muted-box" : undefined,
      }),
    );
    if (c.state === "busy" && c.owner) {
      const gi = NAMES.indexOf(c.owner);
      parts.push(arrow(`own:${i}`, { x: G_X + G_W, y: gY(gi) + 17 }, { x: CONN.x, y: connY(i) + CONN.h / 2 }, { enter: "left" }));
    }
  });

  const slots = [0, 1, 2].map((i) => f.queue[i] ?? null);
  parts.push(memory("queue", { x: Q.x, y: Q.y, values: slots, title: "очередь ожидания", cellW: 60, indexes: false, cls: f.queue.length ? "v-waiting" : undefined }));
  if (f.queueNote) parts.push(label("qnote", Q.x, Q.y + 58, f.queueNote, "v-caption"));

  const open = f.conns.filter((c) => c.state !== "none").length;
  const inUse = f.conns.filter((c) => c.state === "busy" || c.state === "leak").length;
  const idle = f.conns.filter((c) => c.state === "idle").length;
  parts.push(
    struct("stats", {
      x: POOL.x,
      y: 220,
      w: 260,
      title: "db.Stats()",
      fields: [
        ["OpenConnections", open],
        ["InUse", inUse],
        ["Idle", idle],
        ["WaitCount", f.waitCount],
      ],
      hl: f.hl?.filter((h) => ["OpenConnections", "InUse", "Idle", "WaitCount"].includes(h)),
    }),
  );
  return scene(...parts);
}

const none: Conn = { state: "none" };
const busy = (owner: string): Conn => ({ state: "busy", owner });
const leak: Conn = { state: "leak" };
const idle: Conn = { state: "idle" };

const run = (detail = "выполняет запрос"): G => ({ state: "running", detail });
const wait: G = { state: "blocked", detail: "ждёт соединение · стр. 4" };
const done = (detail = "завершилась"): G => ({ state: "done", detail });

export default defineScenario({
  title: "Пул соединений: лимит, очередь и утечка",
  code,
  width: 720,
  height: 370,
  steps: [
    {
      lines: [1],
      note: "`sql.Open` ничего не подключает, а `SetMaxOpenConns(2)` только задаёт лимит: открытых соединений пока нет, `OpenConnections = 0`. Соединения пул откроет сам — по первому требованию.",
      scene: frame({ conns: [none, none], gs: { main: run("настроила пул") }, queue: [], waitCount: 0 }),
    },
    {
      lines: [17, 18, 4],
      note: "Четыре обработчика стартуют одновременно. `g1` вызывает `QueryContext`: простаивающих соединений нет, лимит не достигнут — пул открывает `conn #1` и отдаёт его `g1` до закрытия `rows`.",
      scene: frame({ conns: [busy("g1"), none], gs: { g1: run(), g2: run("запускается"), g3: run("запускается"), g4: run("запускается"), main: run("запустила обработчики") }, queue: [], waitCount: 0, hl: ["g1", "OpenConnections", "InUse"] }),
    },
    {
      lines: [4],
      note: "`g2` получает второе новое соединение. Теперь открыто два из двух — лимит исчерпан.",
      scene: frame({ conns: [busy("g1"), busy("g2")], gs: { g1: run(), g2: run(), g3: run("запускается"), g4: run("запускается"), main: run("запустила обработчики") }, queue: [], waitCount: 0, hl: ["g2", "OpenConnections", "InUse"] }),
    },
    {
      lines: [4],
      note: "`g3` и `g4` тоже вызывают `QueryContext`, но открыть третье соединение нельзя. Горутины встают в очередь ожидания и блокируются, пока соединение не вернут или не кончится их контекст. `WaitCount` считает каждое такое ожидание.",
      scene: frame({ conns: [busy("g1"), busy("g2")], gs: { g1: run(), g2: run(), g3: wait, g4: wait, main: run("запустила обработчики") }, queue: ["g3", "g4"], waitCount: 2, hl: ["g3", "g4", "WaitCount"] }),
    },
    {
      lines: [8, 9],
      note: "У `g2` чётный `id`: она возвращает `errSkip`, не закрыв `rows`. Горутина завершилась, а `conn #2` так и числится занятым — вернуть его в пул может только `rows.Close()`, а ссылку на `rows` больше никто не держит. Это утечка соединения.",
      scene: frame({ conns: [busy("g1"), leak], gs: { g1: run(), g2: done("вышла без rows.Close()"), g3: wait, g4: wait, main: run("запустила обработчики") }, queue: ["g3", "g4"], waitCount: 2, hl: ["g2", "InUse"] }),
    },
    {
      lines: [11, 14],
      note: "`g1` дочитала строки, `defer rows.Close()` вернул `conn #1` в пул. Раз есть ждущие, соединение не попадает в простаивающие, а сразу передаётся одному из них. Выбор случайный, а не по очереди: здесь досталось `g4`.",
      scene: frame({ conns: [busy("g4"), leak], gs: { g1: done(), g2: done("вышла без rows.Close()"), g3: wait, g4: run(), main: run("запустила обработчики") }, queue: ["g3"], waitCount: 2, queueNote: "выбор — случайный", hl: ["g4"] }),
    },
    {
      lines: [8, 9],
      note: "У `g4` тоже чётный `id` — и `conn #1` утекает так же. Оба соединения заняты навсегда, хотя ни одна горутина с ними не работает. Достанься соединение `g3` — она отработала бы и вернула его, но следующей его получила бы `g4` и всё равно потеряла.",
      scene: frame({ conns: [leak, leak], gs: { g1: done(), g2: done("вышла без rows.Close()"), g3: wait, g4: done("вышла без rows.Close()"), main: run("ждёт") }, queue: ["g3"], waitCount: 2, hl: ["g4", "InUse"] }),
    },
    {
      lines: [20, 22, 24, 4],
      note: "Позже приходит новый запрос: `main` вызывает `handle` уже с таймаутом 100 мс и тоже встаёт в очередь: `WaitCount = 3`. Соединений, которые кто-то вернёт, больше нет.",
      scene: frame({ conns: [leak, leak], gs: { g1: done(), g2: done("вышла без rows.Close()"), g3: wait, g4: done("вышла без rows.Close()"), main: wait }, queue: ["g3", "main"], waitCount: 3, hl: ["main", "WaitCount"] }),
    },
    {
      lines: [6, 24],
      note: "Через 100 мс контекст `main` истёк: пул убирает её из очереди, и `QueryContext` возвращает `context deadline exceeded`. А `g3` с контекстом без дедлайна будет ждать вечно — сервис «висит», хотя база свободна.",
      scene: frame({ conns: [leak, leak], gs: { g1: done(), g2: done("вышла без rows.Close()"), g3: { state: "blocked", detail: "ждёт вечно · стр. 4" }, g4: done("вышла без rows.Close()"), main: run("получила ошибку таймаута") }, queue: ["g3"], waitCount: 3, hl: ["main"] }),
    },
    {
      lines: [4, 5, 6, 7, 8, 9, 10, 11],
      note: "Лечение — `defer rows.Close()` сразу после проверки ошибки, до любых ранних `return`. С ним каждый обработчик возвращает соединение, ждущие получают его по очереди, а в конце оба соединения простаивают: `InUse = 0`, `Idle = 2`. Так выглядит пул после исправленного прогона.",
      scene: frame({ conns: [idle, idle], gs: { g1: done(), g2: done(), g3: done(), g4: done(), main: done("получила ответ") }, queue: [], waitCount: 2, hl: ["InUse", "Idle"] }),
    },
  ],
});
