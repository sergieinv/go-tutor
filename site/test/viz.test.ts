// Движок визуализаций: формат viz.ts всех уроков, плеер, построитель шагов select.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { pathToFileURL } from "node:url";
import { ARROW_HEAD, arrow, type Side, validate } from "../../viz/src/index.ts";
import { selectHeight, selectSteps, SELECT_WIDTH } from "../../viz/src/live/select.ts";
import type { TraceResult } from "../../viz/src/live/trace.ts";
import { GOROUTINES_WIDTH, goroutinesHeight, goroutinesSteps, MAX_FRAMES } from "../../viz/src/live/goroutines.ts";
import { SPANS_WIDTH, spansHeight, spansSteps } from "../../viz/src/live/spans.ts";
import { builders } from "../../viz/src/live/index.ts";
import { raceSteps, type RaceResult } from "../../viz/src/live/race.ts";
import { ESCAPE_WIDTH, escapeHeight, escapeSteps, parseEscape } from "../../viz/src/live/escape.ts";
import { type Clock, Player } from "../../viz/src/player.ts";
import type { Step, Viz } from "../../viz/src/types.ts";

const root = path.resolve(import.meta.dirname, "../..");
const content = path.join(root, "content");

const vizFiles = fs
  .readdirSync(content, { withFileTypes: true })
  .filter((m) => m.isDirectory())
  .flatMap((m) =>
    fs
      .readdirSync(path.join(content, m.name), { withFileTypes: true })
      .filter((l) => l.isDirectory())
      .flatMap((l) => ["viz.ts", "viz-live.ts"].map((f) => path.join(content, m.name, l.name, f)))
      .filter((f) => fs.existsSync(f)),
  );

test("у пилотных уроков есть визуализаторы", () => {
  const ids = vizFiles.map((f) => path.relative(content, path.dirname(f)));
  assert.ok(ids.includes("02-basics/05-slices"));
  assert.ok(ids.includes("05-concurrency/07-select"));
});

for (const file of vizFiles) {
  const id = path.relative(content, path.dirname(file));
  test(`${path.basename(file)} ${id} корректен`, async () => {
    const viz: Viz = (await import(pathToFileURL(file).href)).default;
    assert.deepEqual(validate(viz), []);
    if (file.endsWith("viz-live.ts")) assert.equal(viz.kind, "live", "viz-live.ts — только живой визуализатор");
    if (viz.kind === "live") {
      assert.ok(fs.existsSync(path.join(path.dirname(file), viz.file)), `нет ${viz.file}`);
    }
  });
}

const steps = (n: number): Step[] => Array.from({ length: n }, (_, i) => ({ note: `шаг ${i}`, scene: [] }));

class FakeClock implements Clock {
  pending: (() => void) | undefined;
  set(fn: () => void) {
    this.pending = fn;
    return 1;
  }
  clear() {
    this.pending = undefined;
  }
  tick() {
    const fn = this.pending;
    this.pending = undefined;
    fn?.();
  }
}

test("плеер листает шаги в обе стороны и не выходит за края", () => {
  const p = new Player(steps(3));
  const seen: number[] = [];
  p.onChange((pl) => seen.push(pl.index));
  assert.equal(p.prev(), false);
  assert.equal(p.next(), true);
  assert.equal(p.next(), true);
  assert.equal(p.atEnd, true);
  assert.equal(p.next(), false);
  assert.equal(p.prev(), true);
  p.go(99);
  p.go(-5);
  assert.deepEqual(seen, [1, 2, 1, 2, 0]);
});

test("авто доходит до конца и останавливается, с конца начинает заново", () => {
  const clock = new FakeClock();
  const p = new Player(steps(3), clock);
  p.play();
  assert.equal(p.playing, true);
  clock.tick();
  clock.tick();
  assert.equal(p.index, 2);
  assert.equal(p.playing, false);
  assert.equal(clock.pending, undefined);
  p.play();
  assert.equal(p.index, 0);
  p.pause();
  assert.equal(clock.pending, undefined);
});

test("load заменяет шаги и возвращает в начало", () => {
  const p = new Player([]);
  p.load(steps(4));
  p.go(3);
  p.load(steps(2));
  assert.equal(p.index, 0);
  assert.equal(p.steps.length, 2);
});

const fixture: TraceResult = JSON.parse(fs.readFileSync(path.join(root, "viz/test/testdata/select-trace.json"), "utf8"));

test("шаги select строятся по трассе живой программы", () => {
  const t = fixture.trace!;
  const s = selectSteps(t);
  // Старт + по шагу на событие + итог.
  assert.equal(s.length, t.events.length + 2);
  const notes = s.map((x) => x.note);
  assert.match(notes[1], /main запускает горутину G2.*стр\. 15/);
  // main засыпает в select, его будит G2 отправкой со строки 17.
  const wake = s.findIndex((x) => /^Будит G2 \(стр\. 17\)/.test(x.note));
  assert.ok(wake > 0, notes.join("\n"));
  assert.deepEqual(s[wake].lines, [17]);
  assert.ok(s[wake].scene.some((sh) => sh.id === "wake"));
  // Ветка по таймауту: будит рантайм.
  assert.ok(notes.some((n) => n.startsWith("Будит рантайм")));
  assert.ok(notes.some((n) => /`default`/.test(n) && /ни одна ветка не была готова/.test(n)));
  // Итог: G3 не дождалась, счётчики третьего select совпадают с трассой.
  const last = notes[notes.length - 1];
  assert.match(last, /G3 так и не дождалась/);
  const [x, y] = t.selects[2].counts;
  assert.ok(last.includes(`\`case <-a\` — ${x}, \`case <-b\` — ${y}`), last);
  for (const step of s) {
    const ids = step.scene.map((sh) => sh.id);
    assert.equal(new Set(ids).size, ids.length, "повтор id в сцене");
  }
});

test("схема select помещается в свои размеры", () => {
  const t = fixture.trace!;
  const h = selectHeight(t);
  for (const step of selectSteps(t)) {
    for (const sh of step.scene) {
      if (sh.type === "path") continue;
      const right = sh.type === "rect" ? sh.x + sh.w : sh.x;
      const bottom = sh.type === "rect" ? sh.y + sh.h : sh.y;
      assert.ok(right <= SELECT_WIDTH && bottom <= h, `${sh.id}: ${right}×${bottom} вне ${SELECT_WIDTH}×${h}`);
    }
  }
});

test("пустая трасса даёт понятный шаг вместо пустого плеера", () => {
  const s = selectSteps({ goroutines: [{ id: 1, func: "main.main" }], selects: [], events: [], total: 0, durationUs: 5 });
  assert.equal(s.length, 2);
  assert.match(s[0].note, /нет событий/);
});

test("стрелка входит в цель перпендикулярно стороне, остриё — точно в цели", () => {
  const nums = (d: string) => d.match(/-?[\d.]+/g)!.map(Number);
  const cases: [Side, { x: number; y: number }][] = [
    ["top", { x: 0, y: 1 }],
    ["bottom", { x: 0, y: -1 }],
    ["left", { x: 1, y: 0 }],
    ["right", { x: -1, y: 0 }],
  ];
  for (const [enter, dir] of cases) {
    const to = { x: 200, y: 100 };
    const a = arrow("a", { x: 10, y: 20 }, to, { enter });
    assert.equal(a.type, "path");
    const n = nums((a as { d: string }).d);
    const [c2x, c2y, ex, ey] = n.slice(-4);
    // Конец пути — на длину наконечника до цели вдоль направления входа.
    assert.deepEqual([ex + dir.x * ARROW_HEAD, ey + dir.y * ARROW_HEAD], [to.x, to.y], enter);
    // Последний отрезок касательной лежит на той же прямой — маркер смотрит в цель.
    assert.equal(dir.x === 0 ? c2x - ex : c2y - ey, 0, enter);
  }
  // exit: первый отрезок касательной уходит от источника в сторону exit.
  const up = nums((arrow("e", { x: 50, y: 50 }, { x: 150, y: 10 }, { exit: "top", enter: "left" }) as { d: string }).d);
  assert.equal(up[2], 50);
  assert.ok(up[3] < 50);
  // Без enter: цель ниже — вход сверху, на одном уровне — слева.
  const down = nums((arrow("b", { x: 0, y: 0 }, { x: 100, y: 200 }) as { d: string }).d);
  assert.deepEqual(down.slice(-2), [100, 200 - ARROW_HEAD]);
  const level = nums((arrow("c", { x: 0, y: 50 }, { x: 100, y: 55 }) as { d: string }).d);
  assert.deepEqual(level.slice(-2), [100 - ARROW_HEAD, 55]);
});

const readFixture = (name: string) => JSON.parse(fs.readFileSync(path.join(root, "viz/test/testdata", name), "utf8"));

test("горутины: стадии pipeline, передачи между ними и утечка в итоге", () => {
  const t = (readFixture("pipeline-trace.json") as TraceResult).trace!;
  const s = goroutinesSteps(t);
  assert.ok(s.length >= 3 && s.length <= MAX_FRAMES + 2);
  // Стадии сгруппированы по функции и строке go: gen, square и три одинаковые утечки — одна стадия.
  const titles = s[s.length - 1].scene.filter((x) => x.type === "text" && x.id.endsWith(":title") && x.id.startsWith("grp")).map((x) => (x as { text: string }).text);
  assert.deepEqual(titles, ["main", "go func в gen", "go func в square", "go func в main"]);
  // Передачи gen → square видны в тексте шагов и стрелками.
  assert.ok(s.some((x) => /go func в (gen \(стр\. 11\) будит go func в square \(стр\. 22\)|square \(стр\. 22\) будит go func в gen \(стр\. 11\))/.test(x.note)));
  assert.ok(s.some((x) => x.scene.some((sh) => sh.id === "edge:1>2")));
  // Итог: три горутины не завершились, ждут отправки на стр. 43.
  const last = s[s.length - 1];
  assert.match(last.note, /go func в main \(стр\. 40\): 3 — ждут отправки на стр\. 42/);
  assert.deepEqual(last.lines, [42]);
  const h = goroutinesHeight(t);
  for (const step of s)
    for (const sh of step.scene) {
      if (sh.type === "path") continue;
      const right = sh.type === "rect" ? sh.x + sh.w : sh.x;
      const bottom = sh.type === "rect" ? sh.y + sh.h : sh.y;
      assert.ok(right <= GOROUTINES_WIDTH && bottom <= h, `${sh.id}: ${right}×${bottom}`);
    }
});

test("задачи и регионы: цепочка сервисов, остаток дедлайна, отмена третьего", () => {
  const t = (readFixture("spans-trace.json") as TraceResult).trace!;
  const s = spansSteps(t);
  // Вступление, по шагу на событие (3 задачи и 3 региона — начало и конец, 4 лога), итог.
  assert.equal(s.length, 1 + 12 + 4 + 1);
  const names = s[s.length - 1].scene.filter((x) => x.id.startsWith("name:")).map((x) => (x as { text: string }).text);
  assert.deepEqual(names, ["gateway", "orders", "warehouse"]);
  assert.ok(s.some((x) => /orders · deadline: осталось 50 мс/.test(x.note)));
  assert.ok(s.some((x) => /Задача warehouse началась \(вызвана из orders\)/.test(x.note)));
  assert.ok(s.some((x) => /warehouse · error: context deadline exceeded/.test(x.note)));
  const h = spansHeight(t);
  for (const step of s)
    for (const sh of step.scene) {
      if (sh.type === "path") continue;
      const right = sh.type === "rect" ? sh.x + sh.w : sh.x;
      const bottom = sh.type === "rect" ? sh.y + sh.h : sh.y;
      assert.ok(right <= SPANS_WIDTH && bottom <= h, `${sh.id}: ${right}×${bottom}`);
    }
});

test("гонка: три шага на отчёт со строками доступов, без гонок — понятный шаг", () => {
  const r: RaceResult = readFixture("race.json");
  const s = raceSteps(r);
  // Отчёты с одной и той же парой строк схлопываются.
  assert.equal(s.length, 1 + 3);
  assert.match(s[0].note, /Детектор нашёл/);
  assert.deepEqual(s[3].lines, [16, 16]);
  assert.match(s[3].note, /sync\.Mutex/);
  assert.ok(s[3].scene.some((x) => x.id === "acc1:box"));
  assert.ok(!s[1].scene.some((x) => x.id === "acc0:box"));
  const clean = raceSteps({ ...r, races: [], exitCode: 0 });
  assert.equal(clean.length, 1);
  assert.match(clean[0].note, /ничего не нашёл/);
});

// Настоящий вывод go build -gcflags=-m (Go 1.27) для программы из восьми функций.
const escapeOutput = `./main.go:7:6: can inline newPoint
./main.go:25:15: inlining call to newPoint
./main.go:32:13: inlining call to fmt.Println
./main.go:8:2: moved to heap: p
./main.go:12:10: ps does not escape
./main.go:20:11: leaking param: p
./main.go:26:13: make([]point, 8) does not escape
./main.go:30:7: &point{...} escapes to heap
./main.go:32:13: ... argument does not escape
./main.go:32:17: ~r0 escapes to heap
./main.go:32:27: len(dyn) escapes to heap
./main.go:32:27: len(dyn) escapes to heap
./other.go:3:2: moved to heap: q
`;

test("escape: шаг на каждую диагностику main.go по порядку строк, inlining только считается", () => {
  const { diags, inlined } = parseEscape(escapeOutput);
  assert.equal(inlined, 2);
  // Повтор строки и чужой файл отброшены.
  assert.deepEqual(
    diags.map((d) => [d.line, d.kind, d.what]),
    [
      [8, "moved", "p"],
      [12, "stack", "ps"],
      [20, "leak", "p"],
      [26, "stack", "make([]point, 8)"],
      [30, "escapes", "&point{...}"],
      [32, "stack", "... argument"],
      [32, "escapes", "~r0"],
      [32, "escapes", "len(dyn)"],
    ],
  );
  const s = escapeSteps({ buildFailed: false, exitCode: 0, compilerOutput: escapeOutput });
  assert.equal(s.length, 1 + 8);
  assert.match(s[0].note, /в кучу — 5, на стеке — 3; встроено вызовов \(inlining\) — 2/);
  assert.deepEqual(s[1].lines, [8]);
  assert.match(s[1].note, /переезжает в кучу/);
  assert.match(s[5].note, /создаёт объект в куче/);
  assert.match(s[7].note, /результата вызова/);
  assert.match(s[8].note, /интерфейс/);
  const h = escapeHeight(diags);
  for (const step of s)
    for (const sh of step.scene) {
      if (sh.type !== "rect") continue;
      assert.ok(sh.x + sh.w <= ESCAPE_WIDTH && sh.y + sh.h <= h, `${sh.id}`);
    }
  // Текущая диагностика подсвечена, предыдущие — нет.
  assert.ok(s[2].scene.some((x) => x.id === "d12:10:stack" && x.cls?.includes("v-hl")));
  assert.ok(s[2].scene.some((x) => x.id === "d8:2:moved" && !x.cls?.includes("v-hl")));
  const app = escapeSteps({ buildFailed: false, exitCode: 0, compilerOutput: "./main.go:5:9: append escapes to heap\n" });
  assert.match(app[1].note, /выделяет `append`/);
  const empty = escapeSteps({ buildFailed: false, exitCode: 0, compilerOutput: "" });
  assert.equal(empty.length, 1);
});

test("все живые построители зарегистрированы", () => {
  for (const name of ["select", "goroutines", "race", "spans", "escape"]) assert.ok(builders[name], name);
  assert.equal(typeof builders.select.build({ buildFailed: true }), "string");
  assert.equal(typeof builders.goroutines.build({ buildFailed: false, traceError: "нет" }), "string");
});
