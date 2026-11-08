// Загрузка контента курса из ../content при сборке. Формат описан в content/README.md;
// структуру гарантирует tools/lessonlint, поэтому здесь проверок минимум.
import fs from "node:fs";
import path from "node:path";
import YAML from "yaml";

export const contentDir = path.resolve(process.env.CONTENT_DIR ?? path.join(process.cwd(), "..", "content"));

export interface Module {
  slug: string;
  number: number;
  title: string;
  summary: string;
  goals: string[];
  lessons: Lesson[];
}

export interface DocLink {
  title: string;
  url: string;
}

export interface RunnerFlags {
  race?: boolean;
  gomaxprocs?: number;
  godebug?: string;
}

export interface LessonMeta {
  title: string;
  summary: string;
  difficulty: number;
  minutes: number;
  prerequisites?: string[];
  docs?: DocLink[];
  runner?: RunnerFlags;
  /** "fix" — задача «найди и почини»: шаблон рабочий, но уязвимый. */
  taskKind?: "fix";
  /** Песочница показывает кнопку govulncheck; expect проверяет lessonlint. */
  govulncheck?: { expect?: string[] };
}

export interface QuizOption {
  text: string;
  correct?: boolean;
  explain: string;
}

export interface QuizQuestion {
  id: string;
  kind: "output" | "compile" | "single" | "multiple";
  prompt: string;
  code?: string;
  options?: QuizOption[];
  compiles?: boolean;
  explain?: string;
}

export interface Task {
  template: string;
  tests: string;
  hints: string;
}

export interface Example {
  source: string;
  output: string;
  /** Файлы данных рядом с main.go (для embed и т.п.): runner получает их как есть. */
  files: Record<string, string>;
  /** *_test.go: если они есть, пример запускается через go test, а не go run. */
  tests: Record<string, string>;
  /** Свой go.mod примера (например, с уязвимой версией модуля): редактируется в песочнице. */
  mod?: string;
  /** Есть ли в тестах бенчмарки и какие fuzz-цели — для кнопок песочницы. */
  bench: boolean;
  fuzz: string[];
}

// Те же выражения, что в tools/lessonlint/checks.go.
const benchFunc = /^func (Benchmark\w*)\(b \*testing\.B\)/m;
const fuzzFunc = /^func (Fuzz\w*)\(f \*testing\.F\)/gm;

function loadExample(dir: string): Example {
  const names = fs.readdirSync(path.join(dir, "example")).sort();
  const pick = (ok: (f: string) => boolean) =>
    Object.fromEntries(names.filter(ok).map((f) => [f, read(dir, "example", f)]));
  const tests = pick((f) => f.endsWith("_test.go"));
  const testSrc = Object.values(tests).join("\n");
  return {
    source: read(dir, "example", "main.go"),
    output: exists(dir, "example", "output.txt") ? read(dir, "example", "output.txt") : "",
    files: pick((f) => f !== "main.go" && f !== "go.mod" && f !== "output.txt" && !f.endsWith("_test.go")),
    tests,
    mod: names.includes("go.mod") ? read(dir, "example", "go.mod") : undefined,
    bench: benchFunc.test(testSrc),
    fuzz: [...testSrc.matchAll(fuzzFunc)].map((m) => m[1]),
  };
}

export interface Lesson {
  id: string; // "02-basics/05-slices"
  slug: string;
  number: number;
  module: Module;
  meta: LessonMeta;
  body: string; // markdown без frontmatter
  example: Example;
  csharp: { file: string; source: string }[];
  quiz: QuizQuestion[];
  task?: Task;
  hasViz: boolean;
}

const dirPattern = /^(\d{2})-[a-z0-9-]+$/;

const read = (...p: string[]) => fs.readFileSync(path.join(...p), "utf8");
const exists = (...p: string[]) => fs.existsSync(path.join(...p));
const subdirs = (dir: string) =>
  fs
    .readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isDirectory() && dirPattern.test(e.name))
    .map((e) => e.name)
    .sort();

function splitFrontmatter(src: string): { meta: unknown; body: string } {
  const m = /^---\n([\s\S]*?)\n---\n/.exec(src);
  if (!m) throw new Error("lesson.md без frontmatter");
  return { meta: YAML.parse(m[1]), body: src.slice(m[0].length) };
}

let cache: Module[] | undefined;

export function loadCourse(): Module[] {
  if (cache) return cache;
  cache = subdirs(contentDir).map((slug) => {
    const dir = path.join(contentDir, slug);
    const info = YAML.parse(read(dir, "module.yaml"));
    const module: Module = {
      slug,
      number: Number(slug.slice(0, 2)),
      title: info.title,
      summary: info.summary ?? "",
      goals: info.goals ?? [],
      lessons: [],
    };
    module.lessons = subdirs(dir).map((ls) => loadLesson(module, ls));
    return module;
  });
  return cache;
}

function loadLesson(module: Module, slug: string): Lesson {
  const dir = path.join(contentDir, module.slug, slug);
  const { meta, body } = splitFrontmatter(read(dir, "lesson.md"));
  const csharpDir = path.join(dir, "csharp");
  const csharp = exists(csharpDir)
    ? fs
        .readdirSync(csharpDir)
        .filter((f) => f.endsWith(".cs"))
        .sort()
        .map((file) => ({ file, source: read(csharpDir, file) }))
    : [];
  // В сайт попадают только шаблон, открытые тесты и подсказки.
  // hidden_test.go и solution.go остаются на стороне runner.
  const task = exists(dir, "task")
    ? {
        template: read(dir, "task", "task.go"),
        tests: read(dir, "task", "task_test.go"),
        hints: read(dir, "task", "hints.md"),
      }
    : undefined;
  return {
    id: `${module.slug}/${slug}`,
    slug,
    number: Number(slug.slice(0, 2)),
    module,
    meta: meta as LessonMeta,
    body,
    example: loadExample(dir),
    csharp,
    // Варианты вида «true» или «42» YAML читает как bool и число, а сайту нужен текст.
    quiz: (YAML.parse(read(dir, "quiz.yaml")).questions as QuizQuestion[]).map((q) => ({
      ...q,
      options: q.options?.map((o) => ({ ...o, text: String(o.text) })),
    })),
    task,
    hasViz: exists(dir, "viz.ts"),
  };
}

export function allLessons(): Lesson[] {
  return loadCourse().flatMap((m) => m.lessons);
}

export function lessonById(id: string): Lesson | undefined {
  return allLessons().find((l) => l.id === id);
}

export const lessonUrl = (l: Lesson) => `/${l.module.slug}/${l.slug}/`;
