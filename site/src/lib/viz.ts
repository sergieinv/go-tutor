// viz.ts уроков при сборке: сценарии исполняются здесь, в страницу попадают
// только готовые шаги (JSON), а в браузер — общий плеер из viz/.
import type { Viz } from "../../../viz/src/types.ts";

const modules = import.meta.glob<{ default: Viz }>("../../../content/*/*/viz.ts", { eager: true });

const byLesson = new Map(
  Object.entries(modules).map(([file, mod]) => {
    const id = file.replace(/^.*content\//, "").replace(/\/viz\.ts$/, "");
    return [id, mod.default];
  }),
);

export function lessonViz(id: string): Viz | undefined {
  return byLesson.get(id);
}
