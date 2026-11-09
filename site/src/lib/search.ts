// Поиск по курсу в браузере: нормализация, токены, ранжирование и сниппет.
// Индекс строит src/lib/search-index.ts при сборке; здесь — только чистая логика,
// без DOM, чтобы её проверяли юнит-тесты.

/** Документ индекса: страница курса. Поля короткие — индекс грузится целиком. */
export interface SearchDoc {
  /** URL страницы. */
  u: string;
  /** Где страница: «Модуль 5. Конкурентность». */
  m: string;
  /** Заголовок. */
  t: string;
  /** Краткое описание. */
  s: string;
  /** Заголовки разделов. */
  h: string[];
  /** Текст без разметки — из него берётся сниппет. */
  x: string;
  /** Остальные слова страницы, которых нет в x (уже нормализованы, через пробел). */
  w?: string;
}

/** Нижний регистр и ё→е; длина строки не меняется — позиции совпадают с исходным текстом. */
export function normalize(s: string): string {
  let out = "";
  for (const ch of s) {
    const l = ch.toLowerCase();
    out += l === "ё" ? "е" : l.length === ch.length ? l : ch;
  }
  return out;
}

// Слово: буквы, цифры, «_»; идентификаторы с точкой (sync.Once) — одним словом.
const wordRe = /[\p{L}\p{N}_]+(?:\.[\p{L}\p{N}_]+)*/gu;
const cyrillic = /[а-я]/;

// Окончания русских слов, длинные — первыми. Отсечение грубое, но «горутина»,
// «горутины», «горутин» и «горутинами» сводятся к одной основе.
const endings = [
  "ями", "ами", "ого", "его", "ому", "ему", "ыми", "ими", "ешь", "ете", "ите", "ует", "ют", "ут", "ит", "ет",
  "ах", "ях", "ой", "ей", "ом", "ем", "ам", "ям", "ов", "ев", "ую", "юю", "ая", "яя", "ое", "ее", "ые", "ие",
  "ый", "ий", "ых", "их", "ть", "ы", "и", "а", "я", "о", "е", "у", "ю", "ь", "й",
];

/** Основа слова: у русских слов отсекается окончание (основа — не короче трёх букв). */
export function stem(word: string): string {
  if (!cyrillic.test(word)) return word;
  for (const e of endings) {
    if (word.endsWith(e) && word.length - e.length >= 3) return word.slice(0, -e.length);
  }
  return word;
}

interface Token {
  word: string;
  start: number;
  end: number;
}

/** Слова нормализованного текста с позициями; у sync.once — ещё и части sync, once. */
function tokens(norm: string): Token[] {
  const out: Token[] = [];
  for (const m of norm.matchAll(wordRe)) {
    const start = m.index;
    out.push({ word: m[0], start, end: start + m[0].length });
    if (m[0].includes(".")) {
      let pos = start;
      for (const part of m[0].split(".")) {
        out.push({ word: part, start: pos, end: pos + part.length });
        pos += part.length + 1;
      }
    }
  }
  return out;
}

/** Нормализованные слова строки (без позиций). */
export function tokenize(s: string): string[] {
  return tokens(normalize(s)).map((t) => t.word);
}

/** Слово запроса: по чему сравнивать слова документа. */
export interface Term {
  /** Слово запроса как есть (нормализованное). */
  raw: string;
  /** Префикс, с которого должно начинаться слово документа. */
  key: string;
  /** Короткие слова (1–2 символа) ищутся только целиком. */
  exactOnly: boolean;
}

export function parseQuery(q: string): Term[] {
  const seen = new Set<string>();
  const out: Term[] = [];
  for (const raw of tokens(normalize(q)).map((t) => t.word)) {
    if (seen.has(raw)) continue;
    seen.add(raw);
    const st = stem(raw);
    // Короткая основа («гор» от «гору») находила бы лишнее — тогда префикс — само слово.
    const key = st.length >= 4 ? st : raw;
    out.push({ raw, key, exactOnly: raw.length <= 2 });
  }
  // Части идентификатора (sync.once → sync, once) не нужны отдельно: целое слово их покрывает.
  return out.filter((t) => !out.some((o) => o !== t && o.raw.includes(".") && o.raw.split(".").includes(t.raw)));
}

/** 2 — слово совпало (или совпали основы), 1 — совпал префикс, 0 — нет. Основу можно передать готовой. */
export function matchWord(word: string, t: Term, wordStem = stem(word)): 0 | 1 | 2 {
  if (word === t.raw) return 2;
  if (t.exactOnly) return 0;
  if (wordStem === t.key) return 2;
  return word.startsWith(t.key) ? 1 : 0;
}

/** Слово документа с основой — основы считаются один раз при загрузке индекса. */
interface Word {
  w: string;
  st: string;
}

/** Документ, подготовленный к поиску: слова каждого поля. */
export interface Prepared {
  doc: SearchDoc;
  title: Word[];
  heads: Word[];
  summary: Word[];
  module: Word[];
  /** Слова текста и сколько раз каждое встретилось. */
  text: (Word & { n: number })[];
}

const words = (list: string[]): Word[] => [...new Set(list)].map((w) => ({ w, st: stem(w) }));

export function prepare(docs: SearchDoc[]): Prepared[] {
  return docs.map((doc) => {
    const counts = new Map<string, number>();
    for (const w of [...tokenize(doc.x), ...(doc.w ? doc.w.split(" ") : [])]) counts.set(w, (counts.get(w) ?? 0) + 1);
    return {
      doc,
      title: words(tokenize(doc.t)),
      heads: words(doc.h.flatMap(tokenize)),
      summary: words(tokenize(doc.s)),
      module: words(tokenize(doc.m)),
      text: [...counts].map(([w, n]) => ({ w, st: stem(w), n })),
    };
  });
}

const best = (list: Word[], t: Term) => {
  let m: 0 | 1 | 2 = 0;
  for (const { w, st } of list) {
    const r = matchWord(w, t, st);
    if (r > m) m = r;
    if (m === 2) break;
  }
  return m;
};

// Вес поля: заголовок страницы > заголовок раздела > описание > текст.
const weights = { title: [0, 8, 12], heads: [0, 4, 6], summary: [0, 3, 4], module: [0, 1, 2] } as const;

/** Очки документа по одному слову запроса; 0 — слово нигде не встретилось. */
function termScore(p: Prepared, t: Term): number {
  let score = 0;
  for (const field of ["title", "heads", "summary", "module"] as const) {
    score = Math.max(score, weights[field][best(p[field], t)]);
  }
  let hits = 0;
  let exact = false;
  for (const { w, st, n } of p.text) {
    const r = matchWord(w, t, st);
    if (r) hits += n;
    if (r === 2) exact = true;
  }
  if (hits > 0) score += (exact ? 1 : 0.5) + Math.min(hits, 10) * 0.1;
  return score;
}

export interface Hit {
  doc: SearchDoc;
  score: number;
}

/** Документы, где встретились все слова запроса, — лучшие первыми; при равенстве — порядок курса. */
export function search(index: Prepared[], query: string, limit = 8): Hit[] {
  const terms = parseQuery(query);
  if (terms.length === 0) return [];
  const hits: (Hit & { order: number })[] = [];
  index.forEach((p, order) => {
    let score = 0;
    for (const t of terms) {
      const s = termScore(p, t);
      if (s === 0) return;
      score += s;
    }
    hits.push({ doc: p.doc, score, order });
  });
  hits.sort((a, b) => b.score - a.score || a.order - b.order);
  return hits.slice(0, limit).map(({ doc, score }) => ({ doc, score }));
}

/** Кусок текста для вывода: mark — подсветить как совпадение. */
export interface Segment {
  text: string;
  mark: boolean;
}

/** Совпавшие слова текста: [начало, конец) в исходной строке. */
function matches(text: string, terms: Term[]): { start: number; end: number; term: number }[] {
  const out: { start: number; end: number; term: number }[] = [];
  for (const tok of tokens(normalize(text))) {
    const term = terms.findIndex((t) => matchWord(tok.word, t) > 0);
    if (term < 0) continue;
    // sync.once и его часть once совпадают на одном месте — оставляем целое слово.
    const prev = out[out.length - 1];
    if (prev && tok.start < prev.end) continue;
    out.push({ start: tok.start, end: tok.end, term });
  }
  return out;
}

function segments(text: string, ranges: { start: number; end: number }[], from = 0, to = text.length): Segment[] {
  const out: Segment[] = [];
  let pos = from;
  for (const r of ranges) {
    if (r.end <= from || r.start >= to) continue;
    if (r.start > pos) out.push({ text: text.slice(pos, r.start), mark: false });
    out.push({ text: text.slice(Math.max(r.start, from), Math.min(r.end, to)), mark: true });
    pos = Math.min(r.end, to);
  }
  if (pos < to) out.push({ text: text.slice(pos, to), mark: false });
  return out;
}

/** Строка целиком, совпадения подсвечены (заголовок результата). */
export function highlight(text: string, query: string): Segment[] {
  return segments(text, matches(text, parseQuery(query)));
}

/**
 * Сниппет: окно текста около места, где встретилось больше разных слов запроса;
 * совпадения подсвечены, обрезанные края помечены «…». Без совпадений — null.
 */
export function snippet(text: string, query: string, size = 160): Segment[] | null {
  const terms = parseQuery(query);
  const found = matches(text, terms);
  if (found.length === 0) return null;
  let bestAt = 0;
  let bestCount = 0;
  found.forEach((f, i) => {
    const distinct = new Set(found.filter((g) => g.start >= f.start && g.end <= f.start + size * 0.75).map((g) => g.term));
    if (distinct.size > bestCount) {
      bestCount = distinct.size;
      bestAt = i;
    }
  });
  const anchor = found[bestAt].start;
  // Окно начинается немного раньше совпадения — по границе слова.
  let start = Math.max(0, anchor - Math.round(size / 4));
  if (start > 0) {
    const space = text.indexOf(" ", start);
    start = space >= 0 && space < anchor ? space + 1 : anchor;
  }
  let end = Math.min(text.length, start + size);
  if (end < text.length) {
    const space = text.lastIndexOf(" ", end);
    if (space > found[bestAt].end) end = space;
  }
  const out = segments(text, found, start, end);
  if (start > 0) out.unshift({ text: "…", mark: false });
  if (end < text.length) out.push({ text: "…", mark: false });
  return out;
}
