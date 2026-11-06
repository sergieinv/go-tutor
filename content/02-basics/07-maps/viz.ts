// Сценарий «Под капотом»: map в Go 1.24+ — Swiss table.
// Хеши условные: настоящие зависят от случайного seed каждой map.
import { arrow, box, defineScenario, label, memory, scene, struct } from "../../../viz/src/index.ts";

const code = `m := map[string]int{}
m["go"] = 1
m["c#"] = 2
v, ok := m["c#"]
fmt.Println(v, ok)
v, ok = m["rust"]
fmt.Println(v, ok)
for i := range 7 {
	m[string(rune('a'+i))] = i
}
delete(m, "go")
for k := range m {
	fmt.Print(k, " ")
}`;

// Слот группы: ключ, значение и h2 — младшие 7 бит хеша ключа.
type Slot = [key: string, value: number, h2: number] | null;

interface Group {
  slots: Slot[];
  /** Подсвеченные контрольные байты. */
  hlCtrl?: number[];
  /** Подсвеченные слоты. */
  hlSlot?: number[];
}

interface Frame {
  used: number;
  /** null — map без памяти, "small" — одна группа, "table" — таблица из групп. */
  mode: null | "small" | "table";
  groups: Group[];
  hlHeader?: string[];
  hash?: string;
  info?: string;
}

const hex = (n: number) => "0x" + n.toString(16).padStart(2, "0");
const pad = (s: Slot[]): Slot[] => [...s, ...Array<Slot>(8 - s.length).fill(null)];

const GROUP_X = 200;
const CELL_W = 52;
const groupY = [84, 210];

function frame(f: Frame) {
  const hdr = struct("m", {
    x: 10,
    y: 40,
    w: 150,
    title: "m (Map)",
    fields: [
      ["used", f.used],
      ["dirLen", f.mode === "table" ? 1 : 0],
      ["dirPtr", f.mode === null ? "nil" : f.mode === "small" ? "группа" : "каталог"],
    ],
    hl: f.hlHeader,
  });
  const parts: Parameters<typeof scene> = [hdr];
  if (f.mode) {
    const store = box("store", {
      x: 186,
      y: 34,
      w: 446,
      h: f.mode === "small" ? 150 : 262,
      title: f.mode === "small" ? "маленькая map: одна группа на 8 слотов" : "каталог[0] → таблица: 2 группы, 16 слотов",
    });
    parts.push(store, arrow("m:dirPtr", hdr.at("dirPtr"), store.at("left"), { enter: "left" }));
    f.groups.forEach((g, i) => {
      const y = groupY[i];
      parts.push(
        memory(`g${i}ctrl`, {
          x: GROUP_X,
          y,
          cellW: CELL_W,
          cellH: 26,
          indexes: false,
          title: `группа ${i}: контрольное слово (h2 или пусто)`,
          values: g.slots.map((s) => (s ? hex(s[2]) : null)),
          hl: g.hlCtrl,
        }),
        memory(`g${i}slot`, {
          x: GROUP_X,
          y: y + 28,
          cellW: CELL_W,
          cellH: 34,
          values: g.slots.map((s) => (s ? `${s[0]}:${s[1]}` : null)),
          hl: g.hlSlot,
        }),
      );
    });
  }
  if (f.hash) parts.push(label("hash", 10, 322, f.hash, "v-label"));
  if (f.info) parts.push(label("info", 10, 346, f.info, "v-caption"));
  return scene(...parts);
}

// Условные h2 для ключей.
const go: Slot = ["go", 1, 0x2a];
const cs: Slot = ["c#", 2, 0x15];
const a: Slot = ["a", 0, 0x4c];
const b: Slot = ["b", 1, 0x07];
const c: Slot = ["c", 2, 0x63];
const d: Slot = ["d", 3, 0x31];
const e: Slot = ["e", 4, 0x5e];
const f: Slot = ["f", 5, 0x1b];
const g: Slot = ["g", 6, 0x70];

const small2 = pad([go, cs]);
const small8 = [go, cs, a, b, c, d, e, f];
// После роста каждый ключ заново раскладывается: группу выбирает h1.
const t0 = pad([go, b, d, g]);
const t1 = pad([cs, a, c, e, f]);
const t0del = pad([null, b, d, g]);

export default defineScenario({
  title: "map изнутри: Swiss table",
  code,
  width: 640,
  height: 360,
  steps: [
    {
      lines: [1],
      note: "Значение типа map — указатель на структуру `Map`. У пустой map память под элементы обычно ещё не выделена: `dirPtr` пуст, `used` = 0. Хеши на следующих шагах условные — настоящие зависят от случайного seed, который рантайм выбирает для каждой map.",
      scene: frame({ used: 0, mode: null, groups: [], hlHeader: ["used", "dirPtr"] }),
    },
    {
      lines: [2],
      note: "Первая запись выделяет одну группу: 8 слотов ключ/значение и 8-байтовое контрольное слово. Хеш ключа делится на две части: `h1` (старшие 57 бит) и `h2` (младшие 7 бит). `h2` записывается в контрольный байт слота, старший бит байта 0 значит «слот занят».",
      scene: frame({ used: 1, mode: "small", groups: [{ slots: pad([go]), hlCtrl: [0], hlSlot: [0] }], hlHeader: ["used", "dirPtr"], hash: 'hash("go") → h1 = …0110, h2 = 0x2a' }),
    },
    {
      lines: [3],
      note: "Пока элементов не больше 8, map — это одна группа без таблицы и без пробирования: новый ключ занимает первый свободный слот.",
      scene: frame({ used: 2, mode: "small", groups: [{ slots: small2, hlCtrl: [1], hlSlot: [1] }], hlHeader: ["used"], hash: 'hash("c#") → h1 = …1011, h2 = 0x15' }),
    },
    {
      lines: [4, 5],
      note: "Поиск: `h2` = 0x15 сравнивается сразу со всеми 8 контрольными байтами одной побитовой операцией над 64-битным словом. Совпал слот 1 — только для него сравниваются сами ключи. `\"c#\" == \"c#\"`, поэтому `v` = 2, `ok` = true.",
      scene: frame({ used: 2, mode: "small", groups: [{ slots: small2, hlCtrl: [0, 1, 2, 3, 4, 5, 6, 7], hlSlot: [1] }], hash: 'hash("c#") → h2 = 0x15: совпадение в слоте 1', info: "7 бит дают ложное совпадение с вероятностью 1/128 — ключ всё равно сравнивается" }),
    },
    {
      lines: [6, 7],
      note: "`h2(\"rust\")` = 0x61 не совпал ни с одним контрольным байтом — ключа нет, и ни одного сравнения строк не понадобилось. Отсутствующий ключ — не ошибка: map возвращает нулевое значение `0` и `ok` = false.",
      scene: frame({ used: 2, mode: "small", groups: [{ slots: small2, hlCtrl: [0, 1, 2, 3, 4, 5, 6, 7] }], hash: 'hash("rust") → h2 = 0x61: совпадений нет', info: "результат: 0 false" }),
    },
    {
      lines: [8, 9, 10],
      note: "Итерации с `i` от 0 до 5 добавляют ключи `a`…`f`. Группа заполнена: 8 из 8 слотов. Для маленькой map это допустимо — пробировать соседние группы не нужно.",
      scene: frame({ used: 8, mode: "small", groups: [{ slots: small8, hlSlot: [2, 3, 4, 5, 6, 7] }], hlHeader: ["used"], info: "маленькая map: до 8 элементов в одной группе" }),
    },
    {
      lines: [9],
      note: "Девятому ключу `g` места нет. Рантайм создаёт таблицу на 16 слотов (2 группы) и заново раскладывает все ключи: `h1` выбирает стартовую группу, `h2` пишется в контрольный байт. Таблица растёт, когда занято 7/8 её слотов: так при поиске всегда найдётся группа с пустым слотом. Дальше таблица удваивается (32, 64… слотов), а достигнув 1024 слотов, делится на две; нужную таблицу выбирает каталог по старшим битам хеша.",
      scene: frame({ used: 9, mode: "table", groups: [{ slots: t0, hlSlot: [3] }, { slots: t1 }], hlHeader: ["used", "dirLen", "dirPtr"], hash: 'hash("g") → h1 = …0100 → группа 0, h2 = 0x70' }),
    },
    {
      lines: [11],
      note: "`delete` находит ключ так же, как поиск, и освобождает слот. В группе есть пустые слоты, поэтому контрольный байт просто становится «пусто» (0x80). В полностью занятой группе пришлось бы ставить надгробие (0xfe): пустой слот обрывает поиск, и ключи дальше по цепочке групп стали бы ненаходимы.",
      scene: frame({ used: 8, mode: "table", groups: [{ slots: t0del, hlCtrl: [0], hlSlot: [0] }, { slots: t1 }], hlHeader: ["used"], hash: 'hash("go") → h1 = …0110 → группа 0, h2 = 0x2a' }),
    },
    {
      lines: [12, 13, 14],
      note: "`range` обходит слоты таблицы по кругу, но начинает со случайного места: при каждом запуске цикла рантайм берёт случайные смещения. Вместе со случайным seed хеша это делает порядок обхода непредсказуемым — специально, чтобы на него никто не полагался.",
      scene: frame({ used: 8, mode: "table", groups: [{ slots: t0del }, { slots: t1, hlSlot: [2] }], hash: "старт итератора — случайный слот (здесь группа 1, слот 2)", info: "один из возможных выводов: c e f b d g c# a" }),
    },
  ],
});
