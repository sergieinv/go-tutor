// Сценарий «Под капотом»: сетка чанков, вид сверху (z растёт вниз, как
// на карте Minecraft). Игроки A и B с дальностью видимости 1 (квадрат 3×3),
// A ходит через границы чанков и телепортируется; каждая сцена считается
// теми же chunkOf/inView/diff, что и Go-код сценария (проверен запуском).
import { defineScenario, label, scene } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `type ChunkPos struct{ X, Z int }

func chunkOf(x, z float64) ChunkPos {
	return ChunkPos{int(math.Floor(x / 16)), int(math.Floor(z / 16))}
}

func inView(center, c ChunkPos, r int) bool {
	return abs(c.X-center.X) <= r && abs(c.Z-center.Z) <= r
}

// Чанки квадрата to, которых нет в квадрате from.
func diff(from ChunkPos, fromR int, to ChunkPos, toR int) []ChunkPos {
	var out []ChunkPos
	for x := to.X - toR; x <= to.X+toR; x++ {
		for z := to.Z - toR; z <= to.Z+toR; z++ {
			if c := (ChunkPos{x, z}); !inView(from, c, fromR) {
				out = append(out, c)
			}
		}
	}
	return out
}

func main() {
	a := chunkOf(8, 8)
	fmt.Println("A", a, "загрузить", diff(a, -1, a, 1))
	fmt.Println("A", chunkOf(15.9, 8), "тот же чанк")
	a1 := chunkOf(16.1, 8)
	fmt.Println("A", a1, diff(a, 1, a1, 1), diff(a1, 1, a, 1))
	b := chunkOf(-0.5, -0.5)
	fmt.Println("B", b, "загрузить", diff(b, -1, b, 1))
	fmt.Println("A видит B:", inView(a1, b, 1))
	a2 := chunkOf(8, 8)
	fmt.Println("A", a2, diff(a1, 1, a2, 1), diff(a2, 1, a1, 1))
	fmt.Println("A видит B:", inView(a2, b, 1), "B видит A:", inView(b, a2, 1))
	blk := ChunkPos{5 >> 4, -10 >> 4}
	fmt.Println("блок 5,-10:", blk, inView(a2, blk, 1), inView(b, blk, 1))
	a3 := chunkOf(100, 40)
	fmt.Println("A", a3, diff(a2, 1, a3, 1), diff(a3, 1, a2, 1))
	fmt.Println("A r=2:", diff(a3, 1, a3, 2))
}`;

const codeLines = code.split("\n");
/** Номер строки (с 1), содержащей подстроку; падает, если её нет. */
function ln(sub: string): number {
  const i = codeLines.findIndex((l) => l.includes(sub));
  if (i < 0) throw new Error(`нет строки ${sub}`);
  return i + 1;
}
const range = (a: number, b: number) => Array.from({ length: b - a + 1 }, (_, i) => a + i);

// --- та же логика, что в Go-коде ---

interface C {
  x: number;
  z: number;
}
const chunkOf = (x: number, z: number): C => ({ x: Math.floor(x / 16), z: Math.floor(z / 16) });
const inView = (center: C, c: C, r: number) => Math.abs(c.x - center.x) <= r && Math.abs(c.z - center.z) <= r;
function diff(from: C, fromR: number, to: C, toR: number): C[] {
  const out: C[] = [];
  for (let x = to.x - toR; x <= to.x + toR; x++) for (let z = to.z - toR; z <= to.z + toR; z++) if (!inView(from, { x, z }, fromR)) out.push({ x, z });
  return out;
}
const key = (c: C) => `${c.x}:${c.z}`;
const fmtC = (c: C) => `(${c.x},${c.z})`;

// --- геометрия ---

const MINX = -3;
const MAXX = 8;
const MINZ = -3;
const MAXZ = 4;
const CELL = 30;
const GX = 40;
const GY = 50;
const W = 720;
const H = 352;
const PX = 436; // левый край журнала пакетов
const px = (x: number) => +(GX + (x / 16 - MINX) * CELL).toFixed(1);
const pz = (z: number) => +(GY + (z / 16 - MINZ) * CELL).toFixed(1);

interface Player {
  name: string;
  x: number;
  z: number;
  r: number;
}

interface State {
  players: Player[];
  /** Чанки, которые загружаются на этом шаге (подсветка). */
  load?: C[];
  /** Чанки, которые выгружаются на этом шаге (пунктир). */
  unload?: C[];
  /** Призрак: куда положило бы игрока усечение int(x)/16. */
  ghost?: C;
  /** Изменённый блок. */
  block?: { x: number; z: number };
  log: string[];
}

function squarePath(c: C, r: number): string {
  const x0 = GX + (c.x - r - MINX) * CELL;
  const z0 = GY + (c.z - r - MINZ) * CELL;
  const s = (2 * r + 1) * CELL;
  return `M${x0},${z0} H${x0 + s} V${z0 + s} H${x0} Z`;
}

function draw(st: State): Shape[] {
  const out: Shape[] = [];
  const loadK = new Set((st.load ?? []).map(key));
  const unloadK = new Set((st.unload ?? []).map(key));
  const loaded = new Set<string>();
  for (const p of st.players) for (const c of diff(chunkOf(p.x, p.z), -1, chunkOf(p.x, p.z), p.r)) loaded.add(key(c));

  out.push(label("title-grid", GX, 22, "чанки (cx, cz), вид сверху", "v-caption"));
  for (let x = MINX; x <= MAXX; x++) out.push(label(`ax:${x}`, GX + (x - MINX) * CELL + CELL / 2, GY - 6, String(x), "v-index", "middle"));
  for (let z = MINZ; z <= MAXZ; z++) out.push(label(`az:${z}`, GX - 6, GY + (z - MINZ) * CELL + CELL / 2 + 4, String(z), "v-index", "end"));
  for (let x = MINX; x <= MAXX; x++)
    for (let z = MINZ; z <= MAXZ; z++) {
      const k = key({ x, z });
      const cls = loadK.has(k) ? "v-cell v-hl" : unloadK.has(k) ? "v-cell-empty" : loaded.has(k) ? "v-cell" : "v-row";
      out.push({ type: "rect", id: `c:${k}`, x: GX + (x - MINX) * CELL, y: GY + (z - MINZ) * CELL, w: CELL, h: CELL, cls });
    }

  for (const p of st.players) {
    out.push({ type: "path", id: `sq:${p.name}`, d: squarePath(chunkOf(p.x, p.z), p.r), cls: p.name === "A" ? "v-ptr" : "v-ptr v-wake" });
  }
  if (st.ghost) {
    out.push({ type: "path", id: "ghost", d: squarePath(st.ghost, 0), cls: "v-ptr v-wake" });
    out.push(label("ghost-l", GX + (st.ghost.x - MINX) * CELL + 3, GY + (st.ghost.z - MINZ) * CELL + CELL - 4, "B?", "v-wait"));
  }
  if (st.block) {
    out.push({ type: "rect", id: "block", x: px(st.block.x) - 4, y: pz(st.block.z) - 4, w: 8, h: 8, cls: "v-bar" });
  }
  for (const p of st.players) {
    out.push({ type: "circle", id: `p:${p.name}`, x: px(p.x), y: pz(p.z), r: 7, cls: "v-cell v-hl" });
    out.push(label(`pl:${p.name}`, px(p.x), pz(p.z) + 4, p.name, "v-struct-title", "middle"));
  }

  // Легенда под сеткой.
  const LY = GY + (MAXZ - MINZ + 1) * CELL + 22;
  const legend: [string, string, string][] = [
    ["lg-loaded", "v-cell", "загружен"],
    ["lg-load", "v-cell v-hl", "загружается"],
    ["lg-unload", "v-cell-empty", "выгружается"],
  ];
  legend.forEach(([id, cls, text], i) => {
    const x = GX + i * 118;
    out.push({ type: "rect", id, x, y: LY - 11, w: 14, h: 14, cls });
    out.push(label(`${id}-t`, x + 20, LY, text, "v-caption"));
  });

  // Журнал пакетов справа.
  out.push(label("log-title", PX, 22, "что уходит клиентам", "v-caption"));
  st.log.forEach((t, i) => out.push(label(`log:${i}`, PX, GY + 14 + i * 22, t, t.startsWith("  ") ? "v-code" : "v-label")));
  for (const p of st.players) {
    const c = chunkOf(p.x, p.z);
    const i = st.players.indexOf(p);
    out.push(label(`pos:${p.name}`, PX, H - 44 + i * 20, `${p.name}: x=${p.x}, z=${p.z} → чанк ${fmtC(c)}, r=${p.r}`, "v-caption"));
  }
  return out;
}

// --- состояния шагов (координаты — из Go-кода) ---

const A0: Player = { name: "A", x: 8, z: 8, r: 1 };
const A0b: Player = { ...A0, x: 15.9 };
const A1: Player = { ...A0, x: 16.1 };
const B: Player = { name: "B", x: -0.5, z: -0.5, r: 1 };
const A2: Player = { ...A0, x: 8, z: 8 };
const A3: Player = { ...A0, x: 100, z: 40 };
const A4: Player = { ...A3, r: 2 };

const c = (p: Player) => chunkOf(p.x, p.z);
const load1 = diff(c(A0), 1, c(A1), 1);
const unload1 = diff(c(A1), 1, c(A0), 1);
const load2 = diff(c(A1), 1, c(A2), 1);
const unload2 = diff(c(A2), 1, c(A1), 1);
const blockAt = { x: 5, z: -10 };
const blk: C = { x: blockAt.x >> 4, z: blockAt.z >> 4 };
const load3 = diff(c(A2), 1, c(A3), 1);
const unload3 = diff(c(A3), 1, c(A2), 1);
const ring = diff(c(A3), 1, c(A4), 2);
const truncB: C = { x: Math.trunc(B.x) / 16 | 0, z: Math.trunc(B.z) / 16 | 0 };

// Проверки: сценарий обязан совпадать с тем, что печатает Go-код.
const must = (ok: boolean, what: string) => {
  if (!ok) throw new Error(`сценарий разошёлся с кодом: ${what}`);
};
must(key(c(A0b)) === key(c(A0)), "15.9 — тот же чанк");
must(load1.length === 3 && unload1.length === 3, "переход границы — по 3 чанка");
must(key(c(B)) === "-1:-1" && key(truncB) === "0:0", "floor против усечения");
must(!inView(c(A1), c(B), 1), "A пока не видит B");
must(inView(c(A2), c(B), 1) && inView(c(B), c(A2), 1), "A и B видят друг друга");
must(key(blk) === "0:-1" && inView(c(A2), blk, 1) && inView(c(B), blk, 1), "блок видят оба");
must(load3.length === 9 && unload3.length === 9, "телепорт — 9 и 9");
must(ring.length === 16, "кольцо r=2 — 16 чанков");

const col = (cs: C[]) => cs.map(fmtC).join(" ");

export default defineScenario({
  title: "Чанки и зона видимости",
  code,
  width: W,
  height: H,
  steps: [
    {
      lines: [ln("a := chunkOf(8, 8)"), ln('"загрузить", diff(a, -1'), ...range(ln("func chunkOf"), ln("func chunkOf") + 2)],
      note: "A входит в мир в точке (8, 8). `chunkOf` делит координату на 16 и округляет вниз: чанк (0,0). Дальность видимости `r = 1` — квадрат 3×3 вокруг него. `diff` от пустого квадрата (`fromR = -1` не содержит ни одного чанка) — это все 9 чанков: их данные уходят клиенту, и с этого момента A — подписчик каждого из них.",
      scene: scene(draw({ players: [A0], load: diff(c(A0), -1, c(A0), 1), log: ["A ← загрузить 9 чанков", "A ← центр зоны (0,0)", "A подписан на 9 чанков"] })),
    },
    {
      lines: [ln('chunkOf(15.9, 8)')],
      note: "A прошёл почти через весь чанк: x = 15.9 — всё ещё чанк 0. Чанк игрока не изменился — квадрат тот же, загружать и выгружать нечего. Большая часть движений в игре такая: проверка «тот же чанк?» отсекает их одним сравнением. Само движение A, конечно, уходит тем, кто его видит, — но это уже не данные мира.",
      scene: scene(draw({ players: [A0b], log: ["A: (15.9, 8) → чанк (0,0)", "чанк тот же — пакетов нет"] })),
    },
    {
      lines: [ln("a1 := chunkOf(16.1, 8)"), ln("diff(a, 1, a1, 1), diff(a1, 1, a, 1)"), ...range(ln("func diff"), ln("return out"))],
      note: `x = 16.1 — граница пройдена, чанк (1,0). Новый квадрат на 6 чанков совпадает со старым, поэтому \`diff(old, new)\` даёт только полосу справа — ${col(load1)}, а \`diff(new, old)\` — полосу слева: ${col(unload1)}. Слать весь квадрат заново — 9 чанков вместо 3; при дальности 10 это 441 против 21 — в двадцать раз больше трафика на каждый шаг через границу.`,
      scene: scene(draw({ players: [A1], load: load1, unload: unload1, log: ["A ← загрузить 3: столбец x=2", "A ← выгрузить 3: столбец x=-1", "A ← центр зоны (1,0)"] })),
    },
    {
      lines: [ln("b := chunkOf(-0.5, -0.5)"), ln('"B", b, "загрузить"')],
      note: "B входит в точке (−0.5, −0.5). `math.Floor(-0.5/16)` = −1, чанк (−1,−1), и у B свой квадрат. Чанки (0,−1) и (0,0) уже загружены для A, но B получает их отдельно: подписка — у каждого игрока своя, клиенты не делятся данными.",
      scene: scene(draw({ players: [A1, B], load: diff(c(B), -1, c(B), 1), log: ["B ← загрузить 9 чанков", "B ← центр зоны (-1,-1)"] })),
    },
    {
      lines: [ln('"A видит B:", inView(a1, b, 1)'), ln("func inView")],
      note: "Видит ли A игрока B? Да, если чанк B лежит в квадрате A: `inView(a1, b, 1)` — нет, |−1 − 1| = 2 > 1. Пунктир «B?» — где оказался бы B при `int(x)/16`: усечение к нулю даёт 0, чанк (0,0), и A «увидел» бы B, которого не должен видеть. Ошибка проявляется только у отрицательных координат — а хотя бы одна координата отрицательна в трёх четвертях мира.",
      scene: scene(draw({ players: [A1, B], ghost: truncB, log: ["inView(A, чанк B) = false", "int(-0.5)/16 = 0 → (0,0): ошибка", "floor(-0.5/16) = -1 → (-1,-1)"] })),
    },
    {
      lines: [ln("a2 := chunkOf(8, 8)"), ln("diff(a1, 1, a2, 1), diff(a2, 1, a1, 1)"), ln('"B видит A:"')],
      note: "A возвращается в чанк (0,0): загрузить столбец x = −1, выгрузить x = 2. Теперь чанк B в квадрате A, а чанк A — в квадрате B: сервер шлёт каждому «появилась сущность» о другом. С разной дальностью видимость бывает и односторонней — поэтому решают по квадрату того, кто смотрит.",
      scene: scene(draw({ players: [A2, B], load: load2, unload: unload2, log: ["A ← загрузить 3, выгрузить 3", "A ← появился игрок B", "B ← появился игрок A"] })),
    },
    {
      lines: [ln("blk := ChunkPos{5 >> 4, -10 >> 4}"), ln('"блок 5,-10:"')],
      note: "Блок (5, −10) изменён. Его чанк — сдвиг на 4: `5 >> 4` = 0, `-10 >> 4` = −1 (сдвиг знакового целого в Go арифметический, это floor). Событие получают подписчики чанка (0,−1) — A и B, остальным оно не нужно. Если за тик в чанке изменилось несколько блоков, их собирают в один пакет «изменены блоки секции» и шлют в конце тика.",
      scene: scene(draw({ players: [A2, B], block: blockAt, log: ["блок (5,-10) → чанк (0,-1)", "подписчики: A, B", "  A ← блок изменён", "  B ← блок изменён"] })),
    },
    {
      lines: [ln("a3 := chunkOf(100, 40)"), ln("diff(a2, 1, a3, 1), diff(a3, 1, a2, 1)")],
      note: "Телепорт в (100, 40), чанк (6,2). Квадраты не пересекаются, и та же `diff` даёт полный ответ: выгрузить все 9 старых, загрузить все 9 новых — отдельный случай для телепорта не нужен. B больше не видит A: B получает «сущность удалена», A — то же про B.",
      scene: scene(draw({ players: [A3, B], load: load3, unload: unload3, log: ["A ← выгрузить 9, загрузить 9", "A ← центр зоны (6,2)", "A ← удалён игрок B", "B ← удалён игрок A"] })),
    },
    {
      lines: [ln('"A r=2:"')],
      note: "A увеличил дальность до 2: центр тот же, квадрат 5×5. `diff(a3, 1, a3, 2)` — кольцо из 16 чанков вокруг уже загруженных 9. Уменьшение дальности — обратная разность, выгрузка кольца. При дальности 10 квадрат — это 441 чанк (ванильный Minecraft с 1.20.2 считает зону примерно кругом, но порядок тот же), и клиент сообщает свою дальность; поэтому новые чанки шлют порциями по нескольку за тик, ближние первыми.",
      scene: scene(draw({ players: [A4, B], load: ring, log: ["A ← загрузить 16: кольцо r=2", "A ← радиус зоны 2"] })),
    },
  ],
});
