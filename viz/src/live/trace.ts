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

export interface Trace {
  goroutines: TraceGoroutine[];
  selects: TraceSelect[];
  events: TraceEvent[];
  total: number;
  durationUs: number;
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
