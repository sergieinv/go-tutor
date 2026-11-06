// Сценарий «Под капотом»: стек вызовов, отложенные вызовы и раскрутка при panic.
import { box, defineScenario, label, scene } from "../../../viz/src/index.ts";

const code = `func main() {
	q, err := safeDiv(1, 0)
	fmt.Println("main:", q, err)
}

func safeDiv(a, b int) (q int, err error) {
	defer fmt.Println("safeDiv: defer 1")
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("safeDiv: recover")
			err = fmt.Errorf("recovered: %v", r)
		}
	}()
	q = div(a, b)
	return q, nil
}

func div(a, b int) int {
	defer fmt.Println("div: defer")
	return a / b
}`;

// Состояние кадра: строки внутри блока и оформление.
type FrameState = "active" | "unwinding";

interface Frame {
  lines: string[];
  state?: FrameState;
}

interface Frame6 {
  div?: Frame;
  safeDiv?: Frame;
  main: Frame;
  // Паника: поля нет — паники нет; unwinding — идёт раскрутка.
  panic?: { status: string; unwinding?: boolean };
  mode: string;
  out: string[];
}

const STACK_X = 10;
const STACK_W = 350;
// Кадры не двигаются: стек растёт вверх, main — внизу.
const frameGeom = {
  div: { y: 28, h: 72, title: "div(a, b int) int" },
  safeDiv: { y: 110, h: 92, title: "safeDiv(a, b int) (q int, err error)" },
  main: { y: 212, h: 56, title: "main()" },
} as const;

const PANEL_X = 380;
const PANEL_W = 250;

const clsFor = (s?: FrameState) => (s === "active" ? "v-hl" : s === "unwinding" ? "v-waiting" : undefined);

function frame(f: Frame6) {
  const parts: Parameters<typeof scene> = [label("stack:title", STACK_X, 18, "стек горутины main (вершина сверху)", "v-caption")];
  for (const name of ["div", "safeDiv", "main"] as const) {
    const fr = f[name];
    if (!fr) continue;
    const g = frameGeom[name];
    parts.push(box(`frame:${name}`, { x: STACK_X, y: g.y, w: STACK_W, h: g.h, title: g.title, lines: fr.lines, cls: clsFor(fr.state) }));
  }

  parts.push(label("mode:title", PANEL_X, 18, "режим горутины", "v-caption"));
  parts.push(label("mode:value", PANEL_X, 38, f.mode));

  if (f.panic) {
    const lines = ["runtime error:", "integer divide by zero", f.panic.status];
    const cls = f.panic.unwinding ? "v-waiting" : undefined;
    parts.push(box("panic", { x: PANEL_X, y: 56, w: PANEL_W, h: 92, title: "panic", lines, cls }));
  }

  parts.push(box("out", { x: STACK_X, y: 282, w: 620, h: 50 + Math.max(f.out.length, 1) * 18, title: "вывод", lines: f.out }));
  return scene(...parts);
}

// Отложенные вызовы кадра: номер — порядок регистрации, верхний в списке выполнится первым.
const D_CLOSURE = "defer 2: func() { recover() … }";
const D_PRINT = 'defer 1: Println("safeDiv: defer 1")';
const D_DIV = 'defer 1: Println("div: defer")';
const OUT_LAST = "main: 0 recovered: runtime error: integer divide by zero";

export default defineScenario({
  title: "panic, defer и recover на стеке вызовов",
  code,
  width: 640,
  height: 420,
  steps: [
    {
      lines: [2, 6],
      note: "`main` вызывает `safeDiv`: на стеке появляется новый кадр. Именованные результаты `q` и `err` — обычные переменные кадра, сейчас это нулевые значения.",
      scene: frame({
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = nil"], state: "active" },
        main: { lines: ["ждёт результат safeDiv"] },
        mode: "обычное выполнение",
        out: [],
      }),
    },
    {
      lines: [7],
      note: "`defer` кладёт вызов в список отложенных у текущего кадра. Аргумент — строка `\"safeDiv: defer 1\"` — вычислен уже сейчас, а сам `Println` выполнится при выходе из функции.",
      scene: frame({
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = nil", D_PRINT], state: "active" },
        main: { lines: ["ждёт результат safeDiv"] },
        mode: "обычное выполнение",
        out: [],
      }),
    },
    {
      lines: [8, 9, 10, 11, 12, 13],
      note: "Второй `defer` откладывает замыкание с `recover`. Список работает как стек: замыкание легло сверху и выполнится первым. Тело замыкания пока не выполняется.",
      scene: frame({
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = nil", D_CLOSURE, D_PRINT], state: "active" },
        main: { lines: ["ждёт результат safeDiv"] },
        mode: "обычное выполнение",
        out: [],
      }),
    },
    {
      lines: [14, 18, 19],
      note: "`safeDiv` вызывает `div`: новый кадр ложится на вершину стека. У `div` свой список отложенных вызовов — `defer` привязан к функции, которая его выполнила.",
      scene: frame({
        div: { lines: ["a = 1, b = 0", D_DIV], state: "active" },
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = nil", D_CLOSURE, D_PRINT] },
        main: { lines: ["ждёт результат safeDiv"] },
        mode: "обычное выполнение",
        out: [],
      }),
    },
    {
      lines: [20],
      note: "Деление на ноль — рантайм вызывает `panic` со значением типа `runtime.Error`. Обычное выполнение `div` прекращается: `return` не произойдёт, строки после точки паники не выполнятся.",
      scene: frame({
        div: { lines: ["a = 1, b = 0", D_DIV], state: "unwinding" },
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = nil", D_CLOSURE, D_PRINT] },
        main: { lines: ["ждёт результат safeDiv"] },
        panic: { status: "раскрутка: кадр div", unwinding: true },
        mode: "паника — раскрутка стека",
        out: [],
      }),
    },
    {
      lines: [19],
      note: "Начинается раскрутка: рантайм выполняет отложенные вызовы кадра на вершине стека. `div: defer` печатается — `defer` срабатывает и при панике, поэтому через него закрывают ресурсы. `recover` в `div` нет, паника идёт дальше.",
      scene: frame({
        div: { lines: ["a = 1, b = 0", "выполнено: Println(\"div: defer\")"], state: "unwinding" },
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = nil", D_CLOSURE, D_PRINT] },
        main: { lines: ["ждёт результат safeDiv"] },
        panic: { status: "раскрутка: кадр div", unwinding: true },
        mode: "паника — раскрутка стека",
        out: ["div: defer"],
      }),
    },
    {
      lines: [8, 9],
      note: "Кадр `div` снят со стека, раскрутка дошла до `safeDiv`. Первым выполняется верхний отложенный вызов — замыкание. Оно вызывает `recover()`: паника идёт, вызов сделан прямо из отложенной функции, — значит, `recover` вернёт значение паники и остановит раскрутку.",
      scene: frame({
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = nil", "▶ func() { recover() … }", D_PRINT], state: "unwinding" },
        main: { lines: ["ждёт результат safeDiv"] },
        panic: { status: "раскрутка: кадр safeDiv", unwinding: true },
        mode: "паника — раскрутка стека",
        out: ["div: defer"],
      }),
    },
    {
      lines: [10, 11],
      note: "Паника остановлена, горутина снова выполняется обычно. Замыкание печатает строку и записывает ошибку в именованный результат `err` — это единственный способ вернуть что-то из функции, которая паниковала. `q` остался 0: присваивание `q = div(a, b)` так и не случилось.",
      scene: frame({
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = recovered: …", "▶ func() { recover() … }", D_PRINT], state: "active" },
        main: { lines: ["ждёт результат safeDiv"] },
        panic: { status: "остановлена recover()" },
        mode: "обычное выполнение",
        out: ["div: defer", "safeDiv: recover"],
      }),
    },
    {
      lines: [7],
      note: "`safeDiv` всё равно завершается — оставшиеся отложенные вызовы выполняются как при обычном `return`. Печатается `safeDiv: defer 1`. Строки 14–15 не выполняются: после `recover` управление не возвращается в точку паники.",
      scene: frame({
        safeDiv: { lines: ["a = 1, b = 0, q = 0, err = recovered: …", "выполнено: func() { recover() … }", "▶ Println(\"safeDiv: defer 1\")"], state: "active" },
        main: { lines: ["ждёт результат safeDiv"] },
        panic: { status: "остановлена recover()" },
        mode: "обычное выполнение",
        out: ["div: defer", "safeDiv: recover", "safeDiv: defer 1"],
      }),
    },
    {
      lines: [2, 3],
      note: "`safeDiv` возвращает управление `main` нормально, со значениями именованных результатов: `0` и ошибкой. Для `main` паники не было — она превратилась в обычную ошибку. Так `net/http` не даёт панике в одном обработчике уронить весь сервер.",
      scene: frame({
        main: { lines: ["q = 0, err = recovered: runtime error: …"], state: "active" },
        mode: "обычное выполнение",
        out: ["div: defer", "safeDiv: recover", "safeDiv: defer 1", OUT_LAST],
      }),
    },
  ],
});
