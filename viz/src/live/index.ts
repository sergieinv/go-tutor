// Построители живых визуализаторов: какой эндпоинт runner вызвать и как
// превратить ответ в шаги. Имя построителя — поле builder в viz.ts урока.
import type { Step } from "../types.ts";
import { GOROUTINES_WIDTH, goroutinesHeight, goroutinesSteps } from "./goroutines.ts";
import { RACE_HEIGHT, RACE_WIDTH, raceSteps, type RaceResult } from "./race.ts";
import { SELECT_WIDTH, selectHeight, selectSteps } from "./select.ts";
import { SPANS_WIDTH, spansHeight, spansSteps } from "./spans.ts";
import type { TraceResult } from "./trace.ts";

export interface Built {
  steps: Step[];
  width: number;
  height: number;
}

export interface LiveBuilder {
  endpoint: "trace" | "race";
  /** Шаги из ответа runner; строка — почему построить нельзя. */
  build(result: unknown): Built | string;
}

const fromTrace =
  (steps: (t: NonNullable<TraceResult["trace"]>) => Step[], width: number, height: (t: NonNullable<TraceResult["trace"]>) => number) =>
  (result: unknown): Built | string => {
    const r = result as TraceResult;
    if (r.buildFailed) return "Программа не собралась.";
    if (!r.trace) return r.traceError ?? "Трассы нет.";
    return { steps: steps(r.trace), width, height: height(r.trace) };
  };

export const builders: Record<string, LiveBuilder> = {
  select: { endpoint: "trace", build: fromTrace(selectSteps, SELECT_WIDTH, selectHeight) },
  goroutines: { endpoint: "trace", build: fromTrace(goroutinesSteps, GOROUTINES_WIDTH, goroutinesHeight) },
  spans: { endpoint: "trace", build: fromTrace(spansSteps, SPANS_WIDTH, spansHeight) },
  race: {
    endpoint: "race",
    build: (result) => {
      const r = result as RaceResult;
      if (r.buildFailed) return "Программа не собралась.";
      return { steps: raceSteps(r), width: RACE_WIDTH, height: RACE_HEIGHT };
    },
  },
};
