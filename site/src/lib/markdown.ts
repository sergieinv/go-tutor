// Рендер markdown урока в HTML при сборке: тело делится на разделы
// по заголовкам второго уровня, код подсвечивается Shiki.
import rehypeShiki from "@shikijs/rehype";
import type { Root, RootContent } from "mdast";
import rehypeStringify from "rehype-stringify";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { codeToHtml } from "shiki";
import { unified } from "unified";

export const shikiThemes = { light: "github-light", dark: "github-dark" } as const;

const processor = unified()
  .use(remarkParse)
  .use(remarkRehype)
  .use(rehypeShiki, { themes: shikiThemes, defaultLanguage: "go" })
  .use(rehypeStringify);

async function render(children: RootContent[]): Promise<string> {
  const hast = await processor.run({ type: "root", children } satisfies Root);
  return processor.stringify(hast);
}

const headingText = (n: RootContent) =>
  n.type === "heading"
    ? n.children.map((c) => ("value" in c ? c.value : "")).join("").trim()
    : "";

/** Делит узлы на группы по заголовкам заданного уровня. Узлы до первого заголовка — группа с пустым именем. */
function splitBy(nodes: RootContent[], depth: number): { title: string; nodes: RootContent[] }[] {
  const groups = [{ title: "", nodes: [] as RootContent[] }];
  for (const n of nodes) {
    if (n.type === "heading" && n.depth === depth) groups.push({ title: headingText(n), nodes: [] });
    else groups[groups.length - 1].nodes.push(n);
  }
  return groups;
}

export interface Pitfall {
  title: string; // HTML: в заголовках бывает `код`
  html: string;
}

export interface LessonSections {
  intro: string;
  csharp?: string;
  internals?: string;
  pitfalls: Pitfall[];
  task?: string;
}

export async function renderLessonBody(md: string): Promise<LessonSections> {
  const tree = processor.parse(md) as Root;
  const groups = splitBy(tree.children, 2);
  const byTitle = new Map(groups.map((g) => [g.title, g.nodes]));
  const section = (title: string) => {
    const nodes = byTitle.get(title);
    return nodes ? render(nodes) : Promise.resolve(undefined);
  };
  const pitfalls = await Promise.all(
    splitBy(byTitle.get("Ловушки") ?? [], 3)
      .filter((g) => g.title)
      .map(async (g) => ({ title: inlineMarkdown(g.title), html: await render(g.nodes) })),
  );
  return {
    intro: await render(groups[0].nodes),
    csharp: await section("Как это в C#"),
    internals: await section("Под капотом"),
    pitfalls,
    task: await section("Задание"),
  };
}

/** Произвольный markdown целиком: слайды. */
export async function renderMarkdown(md: string): Promise<string> {
  return render((processor.parse(md) as Root).children);
}

/** Подсказки задачи: каждая — раздел «## Подсказка N». */
export async function renderHints(md: string): Promise<string[]> {
  const tree = processor.parse(md) as Root;
  return Promise.all(
    splitBy(tree.children, 2)
      .filter((g) => g.title)
      .map((g) => render(g.nodes)),
  );
}

export function highlight(code: string, lang: "go" | "csharp" | "text"): Promise<string> {
  return codeToHtml(code, { lang, themes: shikiThemes, defaultColor: "light" });
}

const escapeHtml = (s: string) =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

/**
 * Минимальный inline-markdown для коротких строк: только код в обратных
 * кавычках. Как в CommonMark, код с ` внутри пишут в двойных: ``a `b` c``,
 * по одному пробелу у краёв срезается.
 */
export function inlineMarkdown(s: string): string {
  return escapeHtml(s).replace(/(`+)(.+?)\1(?!`)/g, (_, _ticks, code: string) => {
    const inner = code.length > 2 && code.startsWith(" ") && code.endsWith(" ") ? code.slice(1, -1) : code;
    return `<code>${inner}</code>`;
  });
}

/** JSON для <script type="application/json">: `<` экранируется, чтобы код с `</script>` не закрыл тег. */
export function scriptJSON(value: unknown): string {
  return JSON.stringify(value).replace(/</g, "\\u003c");
}
