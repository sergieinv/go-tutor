// Индекс поиска при сборке: модули, уроки, чек-лист ревью и страница /game/.
// Отдаётся как /search-index.json.gz (src/pages/search-index.json.gz.ts) и грузится
// браузером лениво — при первом фокусе в поле поиска.
import fs from "node:fs";
import path from "node:path";
import type { Root, RootContent } from "mdast";
import remarkParse from "remark-parse";
import { unified } from "unified";
import { contentDir, loadCourse, lessonUrl } from "./content";
import { tokenize, type SearchDoc } from "./search";

const parser = unified().use(remarkParse);

/** Сколько символов текста страницы попадает в индекс как есть — для сниппетов. */
export const TEXT_LIMIT = 1500;

/** Текст markdown без разметки и блоков кода; заголовки ## и ### — отдельно. */
export function plainText(md: string): { text: string; headings: string[] } {
  const headings: string[] = [];
  const blocks: string[] = [];
  const inline = (n: RootContent): string =>
    n.type === "text" || n.type === "inlineCode" ? n.value : "children" in n ? (n.children as RootContent[]).map(inline).join("") : "";
  const walk = (nodes: RootContent[]) => {
    for (const n of nodes) {
      if (n.type === "code" || n.type === "html" || n.type === "thematicBreak") continue;
      if (n.type === "heading") {
        const h = inline(n).trim();
        if (n.depth === 2 || n.depth === 3) headings.push(h);
        continue;
      }
      if (n.type === "paragraph") blocks.push(inline(n));
      else if ("children" in n) walk(n.children as RootContent[]);
    }
  };
  walk((parser.parse(md) as Root).children);
  const text = blocks
    .join(" ")
    // Таблицы без remark-gfm остаются абзацами с «|» и строкой «---».
    .replace(/\|/g, " ")
    .replace(/:?-{3,}:?/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  return { text, headings };
}

/**
 * Документ индекса: в x — начало текста (для сниппета), в w — слова остального
 * текста, которых в начале нет. Так поиск видит всю страницу, а индекс не растёт
 * на повторах.
 */
function doc(u: string, m: string, t: string, s: string, h: string[], text: string): SearchDoc {
  if (text.length <= TEXT_LIMIT) return { u, m, t, s, h, x: text };
  let cut = text.lastIndexOf(" ", TEXT_LIMIT);
  if (cut < TEXT_LIMIT / 2) cut = TEXT_LIMIT;
  const x = text.slice(0, cut);
  const seen = new Set(tokenize(x));
  // Числа и слова из 1–2 букв ищутся только целиком и почти ничего не дают — их не храним.
  const rest = [...new Set(tokenize(text.slice(cut)))].filter((w) => !seen.has(w) && w.length > 2 && !/^\d+$/.test(w));
  return { u, m, t, s, h, x, ...(rest.length ? { w: rest.join(" ") } : {}) };
}

export function buildSearchIndex(): SearchDoc[] {
  const docs: SearchDoc[] = [];
  for (const m of loadCourse()) {
    const where = `Модуль ${m.number}. ${m.title}`;
    docs.push(doc(`/#${m.slug}`, "Оглавление", where, m.summary, [], m.goals.join(" ")));
    for (const l of m.lessons) {
      const { text, headings } = plainText(l.body);
      docs.push(doc(lessonUrl(l), where, l.meta.title, l.meta.summary, headings, text));
    }
  }
  const review = plainText(fs.readFileSync(path.join(contentDir, "review-checklist.md"), "utf8"));
  docs.push(
    doc(
      "/review/",
      "Справочник",
      "Чек-лист ревью Go-кода",
      "Что проверять на ревью Go-кода: идиомы, ошибки, context, ресурсы, конкурентность, работа с БД.",
      review.headings,
      review.text,
    ),
  );
  docs.push(
    doc(
      "/game/",
      "Модуль 15",
      "Игровой сервер: браузерный клиент",
      "Браузерный клиент учебного игрового сервера: карта мира, чат и живой визуализатор тика, чанков и зоны видимости.",
      ["Карта мира", "Чат", "Живой визуализатор тика", "Лента событий"],
      "WebSocket-клиент сервера модуля 15: каждый тик сервер присылает пачку событий. Interest management: зона видимости, " +
        "чанки загружаются и выгружаются, MSPT и TPS, события join, leave, spawn, despawn, chunk, teleport, chat.",
    ),
  );
  return docs;
}
