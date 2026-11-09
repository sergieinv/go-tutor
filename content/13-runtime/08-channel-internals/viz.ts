// Сценарий «Под капотом»: select на двух каналах изнутри — порядок опроса
// и порядок блокировки, sudog на каждый канал, прямая передача в стек
// ждущей горутины, гонка за selectDone и снятие с остальных очередей.
import { arrow, box, defineScenario, goroutine, label, memory, scene, struct } from "../../../viz/src/index.ts";
import type { GoroutineState } from "../../../viz/src/index.ts";

const code = `a := make(chan int) // hchan по адресу …2000
b := make(chan int) // hchan по адресу …2060
go func() { // G1
	select {
	case v := <-a:
		fmt.Println("a", v)
	case v := <-b:
		fmt.Println("b", v)
	}
}()
time.Sleep(time.Millisecond) // G1 успевает заснуть
b <- 7`;

interface Sudog {
  elem: string;
  shown: boolean;
}

interface Frame {
  lockA: string;
  lockB: string;
  recvqA: string;
  recvqB: string;
  sgA?: Sudog;
  sgB?: Sudog;
  g1: { state: GoroutineState; detail: string };
  main: { state: GoroutineState; detail: string };
  vA: string | null;
  vB: string | null;
  sel: string[];
  hl?: string[];
  hlSg?: string[];
  hlStack?: number[];
}

const empty = "пусто";
const HA = { x: 10, y: 10 };
const HB = { x: 10, y: 214 };
const SG_X = 225;

function hchan(id: string, at: { x: number; y: number }, name: string, addr: string, lock: string, recvq: string, hl: string[]) {
  return struct(id, {
    x: at.x,
    y: at.y,
    w: 195,
    title: `${name}: runtime.hchan`,
    fields: [
      ["адрес", addr],
      ["dataqsiz", 0],
      ["qcount", 0],
      ["recvq", recvq],
      ["sendq", "пусто"],
      ["lock", lock],
    ],
    hl,
  });
}

function frame(f: Frame) {
  const hl = f.hl ?? [];
  const parts: Parameters<typeof scene> = [];
  const ha = hchan("ha", HA, "a", "…2000", f.lockA, f.recvqA, hl.filter((h) => h.startsWith("a.")).map((h) => h.slice(2)));
  const hb = hchan("hb", HB, "b", "…2060", f.lockB, f.recvqB, hl.filter((h) => h.startsWith("b.")).map((h) => h.slice(2)));
  parts.push(ha, hb);

  const g1 = goroutine("g:G1", { x: 440, y: 10, name: "G1", state: f.g1.state, detail: f.g1.detail, w: 190 });
  const stA = memory("stA", { x: 440, y: 87, values: [f.vA], title: "стек G1: v (case a)", cellW: 120, hl: f.hlStack?.includes(0) ? [0] : [], indexes: false });
  const stB = memory("stB", { x: 440, y: 291, values: [f.vB], title: "стек G1: v (case b)", cellW: 120, hl: f.hlStack?.includes(1) ? [0] : [], indexes: false });
  parts.push(g1, stA, stB);

  const sg = (id: string, y: number, s: Sudog, ch: string) =>
    struct(id, {
      x: SG_X,
      y,
      w: 190,
      title: `sudog (канал ${ch})`,
      fields: [
        ["g", "G1"],
        ["elem", s.elem],
        ["isSelect", "true"],
        ["c", ch],
      ],
      hl: f.hlSg?.includes(id) ? ["g", "elem"] : [],
    });
  if (f.sgA) {
    const s = sg("sgA", 40, f.sgA, "a");
    parts.push(s);
    if (f.recvqA !== empty) parts.push(arrow("qa", ha.at("recvq"), { x: SG_X, y: 53 }, { enter: "left" }));
    parts.push(arrow("ea", s.at("elem"), stA.at(-1), { enter: "left" }));
  }
  if (f.sgB) {
    const s = sg("sgB", 244, f.sgB, "b");
    parts.push(s);
    if (f.recvqB !== empty) parts.push(arrow("qb", hb.at("recvq"), { x: SG_X, y: 257 }, { enter: "left" }));
    parts.push(arrow("eb", s.at("elem"), stB.at(-1), { enter: "left" }));
  }

  parts.push(box("sel", { x: 440, y: 138, w: 190, h: 112, title: "selectgo в G1" }));
  f.sel.forEach((t, i) => parts.push(label(`sel:${i}`, 450, 176 + i * 18, t, "v-caption")));
  parts.push(goroutine("g:main", { x: 440, y: 350, name: "main", state: f.main.state, detail: f.main.detail, w: 190 }));
  parts.push(label("legend", 10, 412, "Адреса условные; lockorder — по адресам hchan, pollorder — случайный.", "v-caption"));
  return scene(...parts);
}

const free = "свободен";
const sleeping = { state: "blocked" as GoroutineState, detail: "time.Sleep" };
const running = { state: "running" as GoroutineState, detail: "выполняется" };

export default defineScenario({
  title: "select изнутри: sudog, порядок блокировки и прямая передача",
  code,
  width: 640,
  height: 422,
  steps: [
    {
      lines: [1, 2],
      note: "Два небуферизованных канала — две структуры `hchan` в куче: `dataqsiz` = 0, буфера нет. Передать значение можно только из рук в руки — от горутины к горутине.",
      scene: frame({ lockA: free, lockB: free, recvqA: empty, recvqB: empty, g1: { state: "runnable", detail: "создана" }, main: running, vA: null, vB: null, sel: [], hl: ["a.dataqsiz", "b.dataqsiz"] }),
    },
    {
      lines: [4],
      note: "`selectgo` сначала строит два порядка. `pollorder` — случайная перестановка веток: так готовые ветки выбираются равновероятно. `lockorder` — ветки, отсортированные по адресу `hchan`: все горутины захватывают мьютексы каналов в одном порядке, поэтому два `select` на тех же каналах не устроят взаимную блокировку.",
      scene: frame({ lockA: free, lockB: free, recvqA: empty, recvqB: empty, g1: running, main: sleeping, vA: null, vB: null, sel: ["pollorder: b, a (случайно)", "lockorder: a, b (по адресу)"]  }),
    },
    {
      lines: [4, 5, 7],
      note: "Проход 1: G1 захватывает мьютексы в `lockorder` — сначала `a`, потом `b` — и опрашивает ветки в `pollorder`: нет ли в `b.sendq` или `a.sendq` ждущего отправителя, не закрыт ли канал. Никого нет, ветки `default` нет — значит, ждать.",
      scene: frame({ lockA: "захвачен G1", lockB: "захвачен G1", recvqA: empty, recvqB: empty, g1: running, main: sleeping, vA: null, vB: null, sel: ["pollorder: b, a", "lockorder: a, b", "проход 1: никто не готов"] , hl: ["a.lock", "b.lock"] }),
    },
    {
      lines: [5, 7],
      note: "Проход 2: на **каждый** канал — свой `sudog`: «горутина G1, значение класть по адресу `elem`, это select». `elem` указывает в стек G1 — на переменную `v` своей ветки. Оба `sudog` встают в `recvq` своих каналов.",
      scene: frame({ lockA: "захвачен G1", lockB: "захвачен G1", recvqA: "sudog G1", recvqB: "sudog G1", sgA: { elem: "→ стек G1", shown: true }, sgB: { elem: "→ стек G1", shown: true }, g1: running, main: sleeping, vA: "?", vB: "?", sel: ["pollorder: b, a", "lockorder: a, b", "проход 2: sudog в оба recvq"] , hl: ["a.recvq", "b.recvq"], hlSg: ["sgA", "sgB"] }),
    },
    {
      lines: [4],
      note: "G1 паркуется (`gopark`), и только после этого мьютексы каналов отпускаются (`selparkcommit`). Горутина не стоит ни в одной очереди планировщика — её знают только два `sudog`. Поток свободен.",
      scene: frame({ lockA: free, lockB: free, recvqA: "sudog G1", recvqB: "sudog G1", sgA: { elem: "→ стек G1", shown: true }, sgB: { elem: "→ стек G1", shown: true }, g1: { state: "blocked", detail: "select: ждёт a или b" }, main: sleeping, vA: "?", vB: "?", sel: ["selectDone = 0", "G1 спит"] , hl: ["a.lock", "b.lock"] }),
    },
    {
      lines: [12],
      note: "`b <- 7`: `main` захватывает мьютекс `b` и снимает голову `b.recvq`. Это `sudog` из select, поэтому сначала нужно выиграть гонку: CAS `selectDone` 0 → 1. Если бы G1 уже разбудил другой канал, CAS не прошёл бы, и `main` взял бы следующего ждущего.",
      scene: frame({ lockA: free, lockB: "захвачен main", recvqA: "sudog G1", recvqB: empty, sgA: { elem: "→ стек G1", shown: true }, sgB: { elem: "→ стек G1", shown: true }, g1: { state: "blocked", detail: "select: ждёт a или b" }, main: { state: "running", detail: "b <- 7: нашла получателя" }, vA: "?", vB: "?", sel: ["selectDone: 0 → 1 (CAS)", "победил канал b"] , hl: ["b.lock", "b.recvq"], hlSg: ["sgB"] }),
    },
    {
      lines: [12],
      note: "Прямая передача (`sendDirect`): 7 копируется из стека `main` прямо в `v` в стеке G1 — буфера нет, промежуточной копии тоже. Это единственное место, где одна работающая горутина пишет в стек другой, поэтому запись идёт с барьером записи GC. G1 будится (`goready`) и встаёт в `runnext` процессора `main`.",
      scene: frame({ lockA: free, lockB: free, recvqA: "sudog G1", recvqB: empty, sgA: { elem: "→ стек G1", shown: true }, g1: { state: "runnable", detail: "разбужена, runnext" }, main: { state: "running", detail: "отправка завершена" }, vA: "?", vB: "7", sel: ["sudog b: success = true"] , hlStack: [1] }),
    },
    {
      lines: [4, 5],
      note: "Проход 3: G1 просыпается внутри `selectgo`, снова захватывает все мьютексы в `lockorder` и снимает свой `sudog` с очередей проигравших каналов — здесь из `a.recvq`. Без этого будущая отправка в `a` нашла бы «получателя», который уже ушёл.",
      scene: frame({ lockA: "захвачен G1", lockB: "захвачен G1", recvqA: empty, recvqB: empty, g1: running, main: { state: "running", detail: "идёт дальше" }, vA: null, vB: "7", sel: ["проход 3: снять sudog", "с a.recvq, вернуть в кеш"] , hl: ["a.recvq", "a.lock", "b.lock"] }),
    },
    {
      lines: [7, 8],
      note: "`selectgo` возвращает номер сработавшей ветки — `b` — и G1 выполняет её тело: значение уже лежит в `v`. Итого: каждый канал select захватил дважды — до сна и после пробуждения, — плюс захват отправителем, два `sudog`, парковка и пробуждение. Поэтому канал заметно дороже мьютекса, хотя и умеет больше.",
      scene: frame({ lockA: free, lockB: free, recvqA: empty, recvqB: empty, g1: { state: "running", detail: "case b: печатает «b 7»" }, main: { state: "running", detail: "идёт дальше" }, vA: null, vB: "7", sel: ["вернул: ветка b"] }),
    },
  ],
});
