// Ответ runner на POST /api/trace (см. runner/livetrace).

export interface TraceGoroutine {
  id: number;
  func: string;
  file?: string;
  line?: number;
}

export interface TraceCase {
  line: number;
  kind: "recv" | "send" | "default";
  text: string;
}

export interface TraceSelect {
  id: number;
  file: string;
  line: number;
  cases: TraceCase[];
  counts: number[];
}

export interface TraceEvent {
  t: number;
  g: number;
  kind: "go" | "block" | "unblock" | "select" | "case" | "end";
  file?: string;
  line?: number;
  reason?: string;
  target?: number;
  select?: number;
  case: number;
}

/** Задача trace.NewTask или регион trace.WithRegion; end -1 — не закрыт. */
export interface TraceSpan {
  id: number;
  kind: "task" | "region";
  name: string;
  /** У задачи — родительская задача (0 — нет), у региона — его задача. */
  task: number;
  g: number;
  start: number;
  end: number;
  depth: number;
}

/** Запись trace.Log(ctx, category, message). */
export interface TraceLog {
  t: number;
  g: number;
  task: number;
  category: string;
  message: string;
}

export interface Trace {
  goroutines: TraceGoroutine[];
  selects: TraceSelect[];
  events: TraceEvent[];
  total: number;
  durationUs: number;
  spans: TraceSpan[];
  logs: TraceLog[];
}

export interface TraceResult {
  buildFailed: boolean;
  buildOutput?: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  timedOut: boolean;
  truncated: boolean;
  durationMs: number;
  trace?: Trace;
  traceError?: string;
}
