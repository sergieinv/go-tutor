// Сценарий «Под капотом»: шардирование остатком от деления против
// consistent hashing. Двенадцать ключей k1…k12, позиции на кольце — CRC-32
// по модулю 360 (градусы), ровно как в коде сценария: сцены считаются этой
// же функцией, поэтому совпадают с запуском кода.
import { arrow, defineScenario, label, memory, scene } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `// Позиция на кольце: 0..359 градусов.
func pos(s string) int { return int(crc32.ChecksumIEEE([]byte(s)) % 360) }

// hash % N: номер шарда зависит от числа шардов.
func byMod(key string, n int) int { return pos(key) % n }

type Ring struct {
	points []int          // отсортированные позиции точек
	owner  map[int]string // точка → шард
}

// Add ставит на кольцо vnodes точек шарда.
func (r *Ring) Add(shard string, vnodes int) {
	for i := range vnodes {
		p := pos(shard + "/" + strconv.Itoa(i))
		r.points = append(r.points, p)
		r.owner[p] = shard
	}
	slices.Sort(r.points)
}

// Get: первая точка по часовой стрелке от позиции ключа.
func (r *Ring) Get(key string) string {
	i, _ := slices.BinarySearch(r.points, pos(key))
	if i == len(r.points) {
		i = 0 // после 359° — снова с начала кольца
	}
	return r.owner[r.points[i]]
}

func main() {
	keys := []string{"k1", "k2", "k3", "k4", "k5", "k6",
		"k7", "k8", "k9", "k10", "k11", "k12"}
	for _, k := range keys {
		fmt.Println(k, pos(k), byMod(k, 3), byMod(k, 4))
	}

	one := &Ring{owner: map[int]string{}}
	for _, s := range []string{"S1", "S2", "S3"} {
		one.Add(s, 1)
	}
	one.Add("S4", 1)

	three := &Ring{owner: map[int]string{}}
	for _, s := range []string{"S1", "S2", "S3"} {
		three.Add(s, 3)
	}
	three.Add("S4", 3)
}`;

const codeLines = code.split("\n");
/** Номер строки (с 1), содержащей подстроку; падает, если её нет. */
function ln(sub: string, from = 1): number {
  const i = codeLines.findIndex((l, j) => j + 1 >= from && l.includes(sub));
  if (i < 0) throw new Error(`нет строки ${sub}`);
  return i + 1;
}
const range = (a: number, b: number) => Array.from({ length: b - a + 1 }, (_, i) => a + i);

// --- та же арифметика, что в Go-коде ---

const CRC = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
function crc32(s: string): number {
  let c = 0xffffffff;
  for (const b of new TextEncoder().encode(s)) c = CRC[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}
const pos = (s: string) => crc32(s) % 360;

const KEYS = Array.from({ length: 12 }, (_, i) => `k${i + 1}`);

interface Point {
  id: string; // "S1/0"
  shard: string;
  p: number;
}
function ring(shards: string[], vnodes: number): Point[] {
  const pts = shards.flatMap((s) => Array.from({ length: vnodes }, (_, i) => ({ id: `${s}/${i}`, shard: s, p: pos(`${s}/${i}`) })));
  if (new Set(pts.map((x) => x.p)).size !== pts.length) throw new Error("совпали позиции точек");
  return pts.sort((a, b) => a.p - b.p);
}
function owner(pts: Point[], key: string): Point {
  const h = pos(key);
  return pts.find((x) => x.p >= h) ?? pts[0];
}
/** Доля кольца каждого шарда в процентах: дуга от предыдущей точки до своей. */
function shares(pts: Point[]): Record<string, number> {
  const out: Record<string, number> = {};
  pts.forEach((x, i) => {
    const prev = pts[(i + pts.length - 1) % pts.length];
    out[x.shard] = (out[x.shard] ?? 0) + ((x.p - prev.p + 360) % 360 || 360);
  });
  for (const k in out) out[k] = Math.round((out[k] / 360) * 100);
  return out;
}

// --- геометрия ---

const W = 720;
const H = 430;
const CX = 205;
const CY = 238;
const R = 135;
const rad = (deg: number) => (deg * Math.PI) / 180;
/** 0° — сверху, по часовой стрелке. */
const at = (deg: number, r: number) => ({ x: +(CX + r * Math.sin(rad(deg))).toFixed(1), y: +(CY - r * Math.cos(rad(deg))).toFixed(1) });
function arcPath(from: number, to: number, r: number): string {
  const sweep = (to - from + 360) % 360;
  const a = at(from, r);
  const b = at(to, r);
  return `M${a.x},${a.y} A${r},${r} 0 ${sweep > 180 ? 1 : 0} 1 ${b.x},${b.y}`;
}

/** Уровни подписей: соседние ближе minGap градусов уходят на следующий уровень. */
function levels(positions: number[], minGap: number, maxLevel: number): number[] {
  const order = positions.map((p, i) => [p, i]).sort((a, b) => a[0] - b[0]);
  const lv: number[] = new Array(positions.length).fill(0);
  let prev = -1000;
  let level = 0;
  for (const [p, i] of order) {
    level = p - prev < minGap ? (level + 1) % maxLevel : 0;
    lv[i] = level;
    prev = p;
  }
  return lv;
}

function ringShapes(pts: Point[], opts: { keys: boolean; hlPoints?: string[]; arc?: [number, number]; walk?: string }): Shape[] {
  const out: Shape[] = [
    { type: "circle", id: "ring", x: CX, y: CY, r: R, cls: "v-row" },
    label("deg0", CX, CY - R - 40, "0° · по часовой →", "v-caption", "middle"),
    { type: "path", id: "tick0", d: `M${CX},${CY - R - 5} L${CX},${CY - R + 5}`, cls: "v-row" },
  ];
  if (opts.arc) out.push({ type: "path", id: "arc", d: arcPath(opts.arc[0], opts.arc[1], R), cls: "v-ptr" });
  const plv = levels(
    pts.map((x) => x.p),
    14,
    3,
  );
  pts.forEach((x, i) => {
    const c = at(x.p, R);
    const hl = opts.hlPoints?.includes(x.shard);
    out.push({ type: "circle", id: `pt:${x.id}`, x: c.x, y: c.y, r: 7, cls: hl ? "v-cell v-hl" : "v-bar" });
    const l = at(x.p, R - 22 - plv[i] * 24);
    out.push(label(`ptl:${x.id}`, l.x, l.y + 4, x.shard, "v-label", "middle"));
  });
  if (opts.keys) {
    const klv = levels(KEYS.map(pos), 9, 2);
    KEYS.forEach((k, i) => {
      const c = at(pos(k), R);
      out.push({ type: "circle", id: `key:${k}`, x: c.x, y: c.y, r: 3.5, cls: "v-cell" });
      const l = at(pos(k), R + 16 + klv[i] * 15);
      out.push(label(`keyl:${k}`, l.x, l.y + 4, k, "v-index", "middle"));
    });
  }
  if (opts.walk) {
    const from = pos(opts.walk);
    const to = owner(pts, opts.walk).p;
    out.push({ type: "path", id: "walk", d: arcPath(from, to, R - 9), cls: "v-ptr v-wake" });
  }
  return out;
}

const BX = 400;
const BW = 305;
const BY = 52;
const BH = 56;
const BGAP = 8;

/** Шарды справа: заголовок с долей кольца и «фишки» ключей. moved — подсвеченные ключи. */
function shardBoxes(shards: string[], assign: Record<string, string> | null, sh: Record<string, number> | null, moved: string[] = [], hlShard?: string): Shape[] {
  const out: Shape[] = [];
  shards.forEach((s, i) => {
    const y = BY + i * (BH + BGAP);
    out.push({ type: "rect", id: `box:${s}`, x: BX, y, w: BW, h: BH, rx: 8, cls: s === hlShard ? "v-box v-waiting" : "v-box" });
    const keys = assign ? KEYS.filter((k) => assign[k] === s) : [];
    const share = sh ? ` · ${sh[s]}% кольца` : "";
    out.push(label(`boxt:${s}`, BX + 10, y + 18, `${s}${share} · ключей: ${keys.length}`, "v-struct-title"));
    keys.forEach((k, j) => {
      const x = BX + 8 + j * 33;
      out.push({ type: "rect", id: `chip:${k}`, x, y: y + 27, w: 30, h: 21, rx: 4, cls: moved.includes(k) ? "v-cell v-hl" : "v-cell" });
      out.push(label(`chipt:${k}`, x + 15, y + 42, k, "v-code", "middle"));
    });
  });
  return out;
}

function assignOf(pts: Point[]): Record<string, string> {
  return Object.fromEntries(KEYS.map((k) => [k, owner(pts, k).shard]));
}
function movedKeys(a: Record<string, string>, b: Record<string, string>): string[] {
  return KEYS.filter((k) => a[k] !== b[k]);
}

const S3 = ["S1", "S2", "S3"];
const S4 = [...S3, "S4"];

// --- шаги 1–2: остаток от деления ---

const mod3 = KEYS.map((k) => pos(k) % 3);
const mod4 = KEYS.map((k) => pos(k) % 4);
const modMoved = KEYS.map((_, i) => i).filter((i) => mod3[i] !== mod4[i]);
const modCount = [0, 1, 2].map((r) => mod3.filter((m) => m === r).length);

function modTable(withFour: boolean): Shape[] {
  const x = 78;
  const cw = 52;
  const out: Shape[] = [];
  const rows: [string, string, (string | number)[], number[]?][] = [
    ["ключ", "keys", KEYS],
    ["pos", "pos", KEYS.map(pos)],
    ["% 3", "m3", mod3],
  ];
  if (withFour) rows.push(["% 4", "m4", mod4, modMoved]);
  rows.forEach(([title, id, values, hl], r) => {
    const y = 70 + r * 62;
    out.push(label(`mt:${id}`, 12, y + 23, title, "v-label"));
    out.push(...memory(id, { x, y, values, cellW: cw, cellH: 34, indexes: false, hl }).shapes);
  });
  return out;
}

// --- шаги на кольце ---

const one3 = ring(S3, 1);
const one4 = ring(S4, 1);
const three3 = ring(S3, 3);
const three4 = ring(S4, 3);
const a13 = assignOf(one3);
const a14 = assignOf(one4);
const a33 = assignOf(three3);
const a34 = assignOf(three4);
const mv1 = movedKeys(a13, a14);
const mv3 = movedKeys(a33, a34);
const sh13 = shares(one3);
const sh14 = shares(one4);
const sh33 = shares(three3);
const sh34 = shares(three4);

// Ключ для шага «как Get идёт по кольцу» — тот, у которого путь до точки самый длинный.
const walkKey = KEYS.reduce((best, k) => {
  const d = (owner(one3, k).p - pos(k) + 360) % 360;
  const db = (owner(one3, best).p - pos(best) + 360) % 360;
  return d > db ? k : best;
});
const walkOwner = owner(one3, walkKey);
const s4one = one4.find((x) => x.shard === "S4")!;
const s4prev = one4[(one4.indexOf(s4one) + one4.length - 1) % one4.length];
const s4next = one4[(one4.indexOf(s4one) + 1) % one4.length];

const fromShards = [...new Set(mv3.map((k) => a33[k]))].sort();
const list = (ks: string[]) => ks.map((k) => `\`${k}\``).join(", ");
const maxKeys = (a: Record<string, string>) => Math.max(...S3.map((s) => KEYS.filter((k) => a[k] === s).length));

function summary(): Shape[] {
  const y = BY + 4 * (BH + BGAP) + 10;
  const rows = [
    `hash % N, 3 → 4 шарда: переехали ${modMoved.length} из 12`,
    `кольцо, 1 точка на шард: ${mv1.length} из 12`,
    `кольцо, 3 точки на шард: ${mv3.length} из 12`,
    `в среднем для кольца: K/(N+1) = 12/4 = 3`,
  ];
  return rows.map((t, i) => label(`sum:${i}`, BX, y + i * 18, t, i === 0 ? "v-wait" : "v-label"));
}

export default defineScenario({
  title: "Шардирование и consistent hashing",
  code,
  width: W,
  height: H,
  steps: [
    {
      lines: [ln("func pos("), ln("func byMod("), ln("byMod(k, 3)")],
      note: `Самый простой роутер — \`pos(key) % N\`. Каждый ключ получает позицию (здесь CRC-32 по модулю 360, чтобы её можно было нарисовать градусами) и номер шарда — остаток от деления на 3. По остаткам 0, 1 и 2 ключи легли как ${modCount.join(", ")}: на дюжине ключей хеш ровности не обещает, она появляется на тысячах. Зато роутер без состояния, и всё хорошо — пока шардов три.`,
      scene: scene(modTable(false)),
    },
    {
      lines: [ln("func byMod("), ln("byMod(k, 3)")],
      note: `Добавили четвёртый шард: делим на 4. Номер изменился у ${modMoved.length} ключей из 12 (подсвечены) — почти все данные надо переносить между шардами, хотя новому шарду достаётся лишь четверть. При переходе с N на N+1 остаётся на месте в среднем только 1/(N+1) ключей: \`% N\` годится, пока число шардов не меняется.`,
      scene: scene(modTable(true), label("modmv", 78, 336, `переехали ${modMoved.length} из 12 ключей`, "v-wait")),
    },
    {
      lines: range(ln("type Ring struct"), ln("slices.Sort(r.points)") + 1).concat([ln("one.Add(s, 1)")]),
      note: `Consistent hashing: позиции ключей и шардов лежат на одном кольце 0…359°. \`Add\` ставит точку шарда в позицию \`pos("S1/0")\` и держит точки отсортированными. Одна точка на шард делит кольцо на дуги очень разной длины: у S1 ${sh13.S1}% кольца, у S2 и S3 — по ${sh13.S2}%.`,
      scene: scene(ringShapes(one3, { keys: false }), shardBoxes(S3, null, sh13)),
    },
    {
      lines: range(ln("func (r *Ring) Get"), ln("return r.owner[r.points[i]]")),
      note: `\`Get\` берёт позицию ключа и бинарным поиском находит первую точку не меньше её — «идёт по часовой стрелке» до ближайшего шарда. Ключ \`${walkKey}\` (${pos(walkKey)}°) доходит до точки ${walkOwner.shard} (${walkOwner.p}°); если ключ стоит после последней точки, поиск возвращает \`len(points)\`, и ключ достаётся первой точке кольца.`,
      scene: scene(ringShapes(one3, { keys: true, walk: walkKey }), shardBoxes(S3, a13, sh13)),
    },
    {
      lines: [ln("one.Add(\"S4\", 1)")],
      note: `Добавили S4 в позицию ${s4one.p}°. Переехали только ключи с дуги между предыдущей точкой (${s4prev.shard}, ${s4prev.p}°) и S4 — ${list(mv1)}. Раньше эта дуга принадлежала следующей точке, ${s4next.shard}, и все ключи с неё ушли именно в S4. Остальные ${12 - mv1.length} ключей остались на месте: в среднем переезжает K/(N+1) ключей, ровно столько, сколько должен получить новый шард.`,
      scene: scene(ringShapes(one4, { keys: true, hlPoints: ["S4"], arc: [s4prev.p, s4one.p] }), shardBoxes(S4, a14, sh14, mv1, "S4")),
    },
    {
      lines: [ln("one.Add(\"S4\", 1)")],
      note: `Но одна точка на шард — это лотерея: доля шарда равна длине его дуги, а дуги случайны. У S3 ${maxKeys(a13)} ключей из 12, а S4 разгрузил только своего соседа по часовой стрелке ${s4next.shard}. Когда шард уходит, всё его хозяйство тоже достаётся одному соседу — он и так был нагружен, теперь вдвое.`,
      scene: scene(ringShapes(one4, { keys: true, hlPoints: ["S3"] }), shardBoxes(S4, a14, sh14, [], "S3")),
    },
    {
      lines: [ln("for i := range vnodes"), ln("p := pos(shard"), ln("three.Add(s, 3)")],
      note: `Виртуальные узлы: каждый шард ставит на кольцо несколько точек — \`S1/0\`, \`S1/1\`, \`S1/2\`. Дуг больше, они короче, и доля шарда — сумма многих случайных дуг, поэтому разброс меньше: ${sh33.S1}%, ${sh33.S2}% и ${sh33.S3}% вместо ${sh13.S1}/${sh13.S2}/${sh13.S3}. В реальных библиотеках точек на шард — 100–200, тогда отклонение долей — порядка 10% и меньше.`,
      scene: scene(ringShapes(three3, { keys: true }), shardBoxes(S3, a33, sh33)),
    },
    {
      lines: [ln("three.Add(\"S4\", 3)")],
      note: `S4 со своими тремя точками забирает по кусочку у разных шардов: переехали ${list(mv3)} — от ${fromShards.join(" и ")}. Нагрузка нового шарда набирается со всего кольца, а при удалении шарда его ключи так же расходятся по нескольким соседям. Ключи по-прежнему переезжают только в S4.`,
      scene: scene(ringShapes(three4, { keys: true, hlPoints: ["S4"] }), shardBoxes(S4, a34, sh34, mv3, "S4")),
    },
    {
      lines: [ln("func (r *Ring) Get"), ln("func byMod(")],
      note: `Итог на двенадцати ключах: \`% N\` перемешал почти всё, кольцо двигает в среднем K/(N+1). На 10 000 ключей из примера урока разница та же: около 80% против 20%. Rendezvous hashing (HRW) даёт то же свойство без кольца — ключ достаётся шарду с наибольшим \`hash(key, shard)\`, а jump hash — без памяти вовсе, но только для шардов с номерами 0…N-1.`,
      scene: scene(ringShapes(three4, { keys: true }), shardBoxes(S4, a34, sh34), summary()),
    },
  ],
});
