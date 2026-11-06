// Клиент runner. Типы повторяют JSON из runner/sandbox и runner/api.

export type Files = Record<string, string>;

export interface Flags {
  race?: boolean;
  gomaxprocs?: number;
  godebug?: string;
}

export interface RunResult {
  buildFailed: boolean;
  buildOutput?: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  timedOut: boolean;
  truncated: boolean;
  durationMs: number;
}

export interface TestCase {
  name: string;
  status: "pass" | "fail" | "skip";
  output: string;
  elapsed: number;
  hidden?: boolean;
}

export interface TestResult {
  buildFailed: boolean;
  buildOutput?: string;
  passed: boolean;
  tests: TestCase[];
  output?: string;
  timedOut: boolean;
  truncated: boolean;
  durationMs: number;
}

export interface FmtResult {
  files: Files;
  errors?: Record<string, string>;
}

/** Ошибка связи с runner или отказ runner (4xx/5xx) — с текстом для ученика. */
export class RunnerError extends Error {}

const unavailable =
  "Runner недоступен. Учебник с песочницей запускается командой docker compose up — " +
  "либо для разработки запустите go run ./runner.";

async function post<T>(path: string, body: unknown): Promise<T> {
  let resp: Response;
  try {
    resp = await fetch(`/api/${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch {
    throw new RunnerError(unavailable);
  }
  // 502/504 отдаёт nginx или прокси dev-сервера, когда runner не запущен.
  if (resp.status === 502 || resp.status === 504) throw new RunnerError(unavailable);
  const data = await resp.json().catch(() => undefined);
  if (!resp.ok) {
    throw new RunnerError(data?.error ?? `Runner ответил ошибкой ${resp.status}`);
  }
  return data as T;
}

export const run = (files: Files, lesson?: string, flags?: Flags) =>
  post<RunResult>("run", { files, lesson, flags });

export const test = (lesson: string, files: Files) => post<TestResult>("test", { lesson, files });

export const format = (files: Files) => post<FmtResult>("fmt", { files });

export const solution = (lesson: string) => post<{ files: Files }>("solution", { lesson });

/** Запуск с runtime/trace для живой визуализации; тип ответа — viz/src/live/trace.ts. */
export const trace = <T>(files: Files, lesson?: string, flags?: Flags) =>
  post<T>("trace", { files, lesson, flags });

/** Запуск с -race для визуализатора гонок; тип ответа — viz/src/live/race.ts. */
export const race = <T>(files: Files, lesson?: string, flags?: Flags) =>
  post<T>("race", { files, lesson, flags });
