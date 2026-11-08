// viz.ts уроков при сборке: сценарии исполняются здесь, в страницу попадают
// только готовые шаги (JSON), а в браузер — общий плеер из viz/.
import type { Viz } from "../../../viz/src/types.ts";

// viz-live.ts — второй, живой визуализатор урока рядом со сценарным viz.ts.
const modules = import.meta.glob<{ default: Viz }>(["../../../content/*/*/viz.ts", "../../../content/*/*/viz-live.ts"], {
  eager: true,
});

export type VizFile = "viz.ts" | "viz-live.ts";

const byFile = new Map(Object.entries(modules).map(([file, mod]) => [file.replace(/^.*content\//, ""), mod.default]));

export function lessonViz(id: string, file: VizFile = "viz.ts"): Viz | undefined {
  return byFile.get(`${id}/${file}`);
}
