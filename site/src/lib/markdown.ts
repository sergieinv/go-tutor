// Рендер markdown урока в HTML при сборке: тело делится на разделы
// по заголовкам второго уровня, код подсвечивается Shiki.
import rehypeShiki from "@shikijs/rehype";
import type { Paragraph, PhrasingContent, Root, RootContent, Table, TableCell, TableRow } from "mdast";
import rehypeStringify from "rehype-stringify";
import remarkParse from "remark-parse";
import remarkRehype from "remark-rehype";
import { codeToHtml } from "shiki";
import { unified } from "unified";

export const shikiThemes = { light: "github-light", dark: "github-dark" } as const;

// Таблицы GFM без remark-gfm: remark-parse оставляет таблицу абзацем, где
// строки разделены переводами строк, а ячейки — «|». Абзац, вторая строка
// которого — разделитель «| --- |», превращается в узел table; HTML из него
// строит remark-rehype. Встроенная разметка в ячейках (`код`, ссылки) сохраняется.
const delimiterRow = /^\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$/;

function tableFromParagraph(p: Paragraph): Table | undefined {
  // Строки абзаца: списки встроенных узлов, разрезанные по \n в текстах.
  const lines: PhrasingContent[][] = [[]];
  for (const node of p.children) {
    if (node.type !== "text") {
      lines[lines.length - 1].push(node);
      continue;
    }
    node.value.split("\n").forEach((part, i) => {
      if (i > 0) lines.push([]);
      if (part) lines[lines.length - 1].push({ type: "text", value: part });
    });
  }
  const text = (line: PhrasingContent[]) => line.map((n) => (n.type === "text" ? n.value : "x")).join("");
  if (lines.length < 2 || !text(lines[0]).trimStart().startsWith("|") || !delimiterRow.test(text(lines[1]))) return;

  const row = (line: PhrasingContent[]): TableRow => {
    const cells: PhrasingContent[][] = [[]];
    for (const node of line) {
      if (node.type !== "text") {
        cells[cells.length - 1].push(node);
        continue;
      }
      node.value.split("|").forEach((part, i) => {
        if (i > 0) cells.push([]);
        if (part) cells[cells.length - 1].push({ type: "text", value: part });
      });
    }
    // Ведущий и замыкающий «|» дают пустые крайние ячейки.
    if (cells.length > 1 && text(cells[0]).trim() === "") cells.shift();
    if (cells.length > 1 && text(cells[cells.length - 1]).trim() === "") cells.pop();
    return {
      type: "tableRow",
      children: cells.map((c): TableCell => {
        const first = c[0];
        const last = c[c.length - 1];
        if (first?.type === "text") first.value = first.value.trimStart();
        if (last?.type === "text") last.value = last.value.trimEnd();
        return { type: "tableCell", children: c.filter((n) => n.type !== "text" || n.value !== "") };
      }),
    };
  };
  return { type: "table", children: [row(lines[0]), ...lines.slice(2).map(row)] };
}

function remarkPipeTables() {
  return (tree: Root) => {
    const walk = (nodes: RootContent[]) => {
      nodes.forEach((n, i) => {
        if (n.type === "paragraph") {
          const table = tableFromParagraph(n);
          if (table) nodes[i] = table;
        } else if ("children" in n && n.type !== "table") walk(n.children as RootContent[]);
      });
    };
    walk(tree.children);
  };
}

const processor = unified()
  .use(remarkParse)
  .use(remarkPipeTables)
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
