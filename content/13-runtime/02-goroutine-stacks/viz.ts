// Сценарий «Под капотом»: рост стека горутины — проверка в прологе,
// morestack, новый стек вдвое больше, копирование с исправлением
// указателей, освобождение старого, сжатие при GC и предел 1 ГБ.
import { arrow, box, defineScenario, label, scene, struct } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `func walk(p *int, n int) {
	var buf [512]byte // кадр walk ≈ 560 байт
	buf[0] = byte(n)
	if n > 0 {
		walk(p, n-1)
	}
	*p += int(buf[0])
}

func main() {
	go func() {
		total := 0
		walk(&total, 4)
		fmt.Println(total) // 10
	}()
	time.Sleep(time.Second)
}`;

// Масштаб: 2 КБ стека — 200 единиц по вертикали.
const SCALE = 200 / 2048;
const TOP = 52;
const COL_W = 150;
const OLD_X = 40;
const NEW_X = 250;
const GUARD = 928; // stackGuard в linux/amd64: 800 + 128 байт
const FUNC1 = 200; // кадр func1, байт
const WALK = 560; // кадр walk, байт

type Col = "old" | "new";

interface Stack {
  col: Col;
  size: number; // байт
  /** Кадры сверху вниз (от hi к lo): имена. */
  frames: string[];
  cls?: string;
  title: string;
}

interface Frame {
  stacks: Stack[];
  g: [string, string][];
  p: string;
  /** Где указатель p: в каком столбце лежат кадры func1 и walk(4). */
  ptr?: Col;
  /** Пояснения в правой колонке. */
  info: string[];
  sp?: Col;
  hl?: string[];
  hlG?: string[];
}

const colX = (c: Col) => (c === "old" ? OLD_X : NEW_X);
const frameSize = (f: string) => (f === "func1" ? FUNC1 : WALK);
const frameTitle = (f: string) => (f === "func1" ? "func1 · total" : `${f} · buf, p`);

function frameY(frames: string[], i: number) {
  let off = 0;
  for (let k = 0; k < i; k++) off += frameSize(frames[k]);
  return TOP + off * SCALE;
}

function drawStack(s: Stack, hl: Set<string>, ghost: boolean): Shape[] {
  const x = colX(s.col);
  const h = s.size * SCALE;
  const pfx = ghost ? `ghost:${s.col}:` : "";
  const shapes: Shape[] = [
    { type: "rect", id: `${s.col}:box`, x, y: TOP, w: COL_W, h, rx: 4, cls: ["v-box", s.cls].filter(Boolean).join(" ") },
    { type: "text", id: `${s.col}:title`, x, y: TOP - 10, text: s.title, cls: "v-caption" },
    { type: "text", id: `${s.col}:hi`, x: x - 18, y: TOP + 12, text: "hi", cls: "v-caption" },
    { type: "text", id: `${s.col}:lo`, x: x - 18, y: TOP + h, text: "lo", cls: "v-caption" },
  ];
  if (!ghost) {
    const gy = TOP + h - GUARD * SCALE;
    shapes.push({ type: "path", id: `${s.col}:guard`, d: `M${x},${gy} H${x + COL_W}`, cls: "v-ptr" });
    shapes.push({ type: "text", id: `${s.col}:guardText`, x: x + 6, y: gy + 15, text: "stackguard0", cls: "v-caption" });
  }
  s.frames.forEach((f, i) => {
    const y = frameY(s.frames, i);
    const fh = frameSize(f) * SCALE;
    const cls = ghost ? "v-cell v-cell-empty" : hl.has(f) ? "v-cell v-hl" : "v-cell";
    shapes.push({ type: "rect", id: `${pfx}f:${f}`, x: x + 4, y, w: COL_W - 8, h: fh, rx: 3, cls });
    shapes.push({ type: "text", id: `${pfx}t:${f}`, x: x + 12, y: y + Math.min(fh / 2 + 5, 15), text: frameTitle(f), cls: ghost ? "v-caption" : "v-value" });
  });
  return shapes;
}

function frame(f: Frame) {
  const hl = new Set(f.hl ?? []);
  const parts: Parameters<typeof scene> = [];
  for (const s of f.stacks) parts.push(drawStack(s, hl, s.cls === "v-muted-box"));

  // Указатель p из кадра walk(4) на total в кадре func1.
  const live = f.stacks.find((s) => s.col === f.ptr && s.cls !== "v-muted-box");
  if (live && live.frames.includes("walk(4)")) {
    const x = colX(live.col) + COL_W - 4;
    const from = { x, y: frameY(live.frames, 1) + 20 };
    const to = { x, y: frameY(live.frames, 0) + 10 };
    parts.push(arrow("p:ptr", from, to, { exit: "right", enter: "right" }));
  }
  if (f.sp) {
    const s = f.stacks.find((st) => st.col === f.sp && st.cls !== "v-muted-box");
    if (s) {
      let off = 0;
      for (const fr of s.frames) off += frameSize(fr);
      parts.push(label("sp", colX(s.col) - 26, TOP + off * SCALE + 4, "SP→", "v-caption"));
    }
  }

  parts.push(struct("g", { x: 440, y: TOP, w: 190, title: "g — горутина", fields: f.g, hl: f.hlG }));
  parts.push(struct("pv", { x: 440, y: 220, w: 190, title: "p в кадре walk(4)", fields: [["p", f.p]], hl: f.hlG?.includes("p") ? ["p"] : [] }));
  parts.push(box("info", { x: 440, y: 286, w: 190, h: 164, title: "что происходит" }));
  f.info.forEach((t, i) => parts.push(label(`info:${i}`, 450, 324 + i * 18, t, "v-caption")));
  return scene(...parts);
}

const g2 = (sp: string): [string, string][] => [
  ["stack.lo", "0x1000"],
  ["stack.hi", "0x1800"],
  ["stackguard0", "0x13a0"],
  ["размер", "2 КБ"],
  ["sched.sp", sp],
];
const g4 = (sp: string, size = "4 КБ"): [string, string][] => [
  ["stack.lo", "0x4000"],
  ["stack.hi", "0x5000"],
  ["stackguard0", "0x43a0"],
  ["размер", size],
  ["sched.sp", sp],
];

const old = (frames: string[], cls?: string, title = "старый стек, 2 КБ"): Stack => ({ col: "old", size: 2048, frames, cls, title });
const neu = (frames: string[], title = "новый стек, 4 КБ"): Stack => ({ col: "new", size: 4096, frames, title });
const W4 = ["func1", "walk(4)"];
const ALL = ["func1", "walk(4)", "walk(3)", "walk(2)", "walk(1)", "walk(0)"];

export default defineScenario({
  title: "Рост стека горутины",
  code,
  width: 640,
  height: 470,
  steps: [
    {
      lines: [11],
      note: "`go func()` создаёт горутину со стеком 2 КБ (адреса на схеме условные). Стек растёт вниз: от `hi` к `lo`. В структуре горутины рантайм хранит границы стека и `stackguard0` — отметку чуть выше `lo`: 928 байт в linux/amd64 оставлены на функции без проверки стека и на запас.",
      scene: frame({ stacks: [old([])], g: g2("0x1800"), p: "—", info: ["стек выделен из кеша", "стеков текущего P;", "размер — стартовый:", "2 КБ, пока GC не", "насчитал больший"], hlG: ["stack.lo", "stack.hi", "stackguard0"] }),
    },
    {
      lines: [12, 13],
      note: "Кадр `func1` с переменной `total`, затем вызов `walk(&total, 4)`. Пролог каждой функции сравнивает указатель стека с `stackguard0`: кадр `walk` (~560 байт) помещается — тело выполняется. Указатель `p` в кадре `walk(4)` смотрит на `total` в кадре ниже по вызовам: это указатель **внутрь стека**.",
      scene: frame({ stacks: [old(W4)], g: g2("0x1508"), p: "0x17f0", ptr: "old", sp: "old", info: ["пролог walk:", "SP − 560 ≥ stackguard0", "→ места хватает"], hl: ["walk(4)"], hlG: ["sched.sp"] }),
    },
    {
      lines: [1, 5],
      note: "`walk(p, 3)`: пролог видит, что новый кадр опустит SP ниже `stackguard0`. Тело не выполняется — вместо него вызывается `runtime.morestack`. Он переключается на системный стек потока (g0) и вызывает `newstack`.",
      scene: frame({ stacks: [old(W4)], g: g2("0x1508"), p: "0x17f0", ptr: "old", sp: "old", info: ["пролог walk(3):", "SP − 560 < stackguard0", "→ morestack → newstack", "(на стеке потока g0)"], hlG: ["stackguard0", "sched.sp"] }),
    },
    {
      lines: [1],
      note: "`newstack` выделяет стек вдвое больше — 4 КБ (если кадру нужно больше, удваивает ещё). Предел — 1 ГБ на 64-битных системах (`debug.SetMaxStack`). Горутина на время копирования переходит в состояние `_Gcopystack`, и сборщик мусора её стек не трогает.",
      scene: frame({ stacks: [old(W4), neu([])], g: g2("0x1508"), p: "0x17f0", ptr: "old", sp: "old", info: ["newsize = 2 × 2 КБ", "stackalloc: 4 КБ", "проверка: не больше", "maxstacksize (1 ГБ)"], hlG: ["размер"] }),
    },
    {
      lines: [1],
      note: "`copystack` копирует занятую часть старого стека в верх нового — байт в байт, так что `hi` нового стека соответствует `hi` старого. Все адреса внутри стека сдвинулись на одну и ту же дельту: 0x5000 − 0x1800 = 0x3800.",
      scene: frame({ stacks: [old(W4, "v-muted-box"), neu(W4)], g: g2("0x1508"), p: "0x17f0", ptr: "new", info: ["memmove(new.hi − used,", "         old.hi − used,", "         used)", "дельта = 0x3800"], hl: ["func1", "walk(4)"] }),
    },
    {
      lines: [1],
      note: "Исправление указателей: по картам стека (какие слова кадра — указатели) рантайм проходит каждый кадр и сдвигает на дельту все указатели, которые смотрели в старый стек: `p` из 0x17f0 становится 0x4ff0. Так же правятся сохранённые указатели кадров, записи `defer` и `panic`, ожидания каналов.",
      scene: frame({ stacks: [old(W4, "v-muted-box"), neu(W4)], g: g2("0x1508"), p: "0x4ff0", ptr: "new", info: ["adjustpointers:", "0x17f0 + 0x3800 = 0x4ff0", "из кучи в стек указателей", "нет — их не пустил", "escape analysis"], hlG: ["p"] }),
    },
    {
      lines: [1, 5],
      note: "Старый стек возвращается в кеш (`stackfree`), в `g` записаны новые границы и `stackguard0`. `morestack` возвращает управление в начало `walk(3)` — пролог проверяет стек заново, и теперь кадр помещается.",
      scene: frame({ stacks: [old([], "v-muted-box", "старый стек — освобождён"), neu(["func1", "walk(4)", "walk(3)"])], g: g4("0x4ad8"), p: "0x4ff0", ptr: "new", sp: "new", info: ["stackfree(старый)", "пролог walk(3) ещё раз:", "SP − 560 ≥ stackguard0"], hl: ["walk(3)"], hlG: ["stack.lo", "stack.hi", "stackguard0", "размер"] }),
    },
    {
      lines: [2, 3, 4, 5],
      note: "Рекурсия продолжается до `walk(0)`: шесть кадров — около 3 КБ — помещаются в 4 КБ над отметкой. Новых копирований нет: стек удваивается, поэтому на глубину N приходится O(log N) переездов, а суммарная работа по копированию — O(N).",
      scene: frame({ stacks: [neu(ALL)], g: g4("0x4448"), p: "0x4ff0", ptr: "new", sp: "new", info: ["200 + 5 × 560 = 3000 байт", "до stackguard0 —", "4096 − 928 = 3168"], hl: ["walk(0)"], hlG: ["sched.sp"] }),
    },
    {
      lines: [7, 14],
      note: "Возврат: каждый кадр выполняет `*p += int(buf[0])` через исправленный указатель и уходит со стека. `total` = 4 + 3 + 2 + 1 + 0 = 10. Код не заметил, что его стек переехал.",
      scene: frame({ stacks: [neu(["func1"])], g: g4("0x4f38"), p: "—", info: ["кадры walk сняты", "total = 10", "Println(total)"], hl: ["func1"], hlG: ["sched.sp"] }),
    },
    {
      lines: [14],
      note: "Сжатие: если во время GC горутина занимает меньше четверти своего стека, сборщик ужимает его вдвое тем же копированием — но не меньше 2 КБ. Так глубокая, но кратковременная рекурсия не держит память вечно. Отключается `GODEBUG=gcshrinkstackoff=1`.",
      scene: frame({ stacks: [old(["func1"], undefined, "стек после сжатия, 2 КБ")], g: g2("0x1738"), p: "—", info: ["shrinkstack при GC:", "used < 4 КБ / 4", "→ copystack(2 КБ)"], hl: ["func1"], hlG: ["размер", "stack.lo", "stack.hi"] }),
    },
    {
      lines: [5],
      note: "Если бы рекурсия не кончалась, стек удваивался бы до предела `maxstacksize` — 10⁹ байт. Удвоение с 512 МиБ даёт 1 ГиБ, это уже больше лимита, и `newstack` печатает `runtime: goroutine stack exceeds 1000000000-byte limit` и `fatal error: stack overflow`. Это не паника: `recover` не сработает, процесс завершится с кодом 2.",
      scene: frame({ stacks: [neu(ALL, "стек растёт: 4, 8, …, 512 МБ")], g: [["stack.lo", "…"], ["stack.hi", "…"], ["stackguard0", "…"], ["размер", "512 МБ"], ["sched.sp", "…"]], p: "0x4ff0", ptr: "new", info: ["newsize = 1 ГБ >", "maxstacksize = 10⁹", "→ throw(\"stack overflow\")", "recover не поможет"], hlG: ["размер"] }),
    },
  ],
});
