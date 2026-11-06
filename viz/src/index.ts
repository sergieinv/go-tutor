// Публичный API движка визуализаций для viz.ts уроков.
import type { LiveSpec, Scenario, Viz } from "./types.ts";

export * from "./primitives.ts";
export type * from "./types.ts";

export const defineScenario = (s: Omit<Scenario, "kind">): Scenario => ({ kind: "scenario", ...s });
export const defineLive = (s: Omit<LiveSpec, "kind">): LiveSpec => ({ kind: "live", ...s });

/** Проверка viz.ts: возвращает список проблем (пусто — всё хорошо). */
export function validate(v: Viz): string[] {
  const problems: string[] = [];
  if (!v.title) problems.push("нет title");
  if (v.kind === "live") {
    if (v.builder !== "select") problems.push(`неизвестный builder ${v.builder}`);
    if (!v.file.endsWith(".go")) problems.push("file должен указывать на .go-файл");
    return problems;
  }
  const lines = v.code.trimEnd().split("\n").length;
  if (v.steps.length < 2) problems.push("меньше двух шагов");
  v.steps.forEach((s, i) => {
    const n = i + 1;
    if (!s.note.trim()) problems.push(`шаг ${n}: нет описания`);
    for (const l of s.lines ?? []) if (l < 1 || l > lines) problems.push(`шаг ${n}: строка ${l} вне кода (строк ${lines})`);
    const ids = new Set<string>();
    for (const sh of s.scene) {
      if (ids.has(sh.id)) problems.push(`шаг ${n}: повтор id ${sh.id}`);
      ids.add(sh.id);
      const xs = sh.type === "path" ? [] : [sh.x];
      const ys = sh.type === "path" ? [] : [sh.y];
      if (sh.type === "rect") xs.push(sh.x + sh.w), ys.push(sh.y + sh.h);
      if (xs.some((x) => x < 0 || x > v.width) || ys.some((y) => y < 0 || y > v.height))
        problems.push(`шаг ${n}: фигура ${sh.id} за пределами ${v.width}×${v.height}`);
    }
  });
  return problems;
}
