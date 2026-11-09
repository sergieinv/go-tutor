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

export type GoTestMode = "test" | "bench" | "fuzz" | "cover";

/** Вывод go test как есть; повторяет sandbox.GoTestResult. */
export interface GoTestResult extends RunResult {
  /** Отчёт go tool cover -func (режим cover). */
  coverage?: string;
  /** Входы из testdata/fuzz, на которых фаззер нашёл ошибку. */
  fuzzInputs?: { name: string; data: string }[];
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

/** go test для примера с тестами: файлы ученика целиком, включая _test.go. */
export const goTest = (files: Files, mode: GoTestMode, lesson?: string, fuzz?: string) =>
  post<GoTestResult>("gotest", { files, lesson, mode, fuzz });

/** Отчёт govulncheck; повторяет sandbox.VulncheckResult. */
export interface VulncheckResult {
  output: string;
  exitCode: number;
  /** Код вызывает уязвимые функции. */
  found: boolean;
  /** govulncheck не смог проверить код. */
  failed: boolean;
  timedOut: boolean;
  /** Дата снимка базы уязвимостей в образе runner, ГГГГ-ММ-ДД. */
  dbModified?: string;
  durationMs: number;
}

/** govulncheck по модулю примера: код не запускается, только анализируется. */
export const vulncheck = (files: Files, lesson?: string) => post<VulncheckResult>("vulncheck", { files, lesson });

/** Вывод buf lint / buf breaking; повторяет sandbox.BufResult. */
export interface BufResult {
  output: string;
  exitCode: number;
  /** buf нашёл нарушения. */
  found: boolean;
  /** buf не смог проверить: .proto не компилируется и т.п. */
  failed: boolean;
  timedOut: boolean;
  durationMs: number;
}

/** buf lint, а с against — buf breaking против прежней версии контракта. */
export const buf = (files: Files, lesson?: string, against?: Files) => post<BufResult>("buf", { files, lesson, against });

export const format = (files: Files) => post<FmtResult>("fmt", { files });

export const solution = (lesson: string) => post<{ files: Files }>("solution", { lesson });

/** Запуск с runtime/trace для живой визуализации; тип ответа — viz/src/live/trace.ts. */
export const trace = <T>(files: Files, lesson?: string, flags?: Flags) =>
  post<T>("trace", { files, lesson, flags });

/** Запуск с -race для визуализатора гонок; тип ответа — viz/src/live/race.ts. */
export const race = <T>(files: Files, lesson?: string, flags?: Flags) =>
  post<T>("race", { files, lesson, flags });
