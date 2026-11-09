// Поиск по курсу: нормализация, основы русских слов, идентификаторы Go,
// ранжирование по полям, AND по словам запроса и сниппет с подсветкой.
import assert from "node:assert/strict";
import { test } from "node:test";
import { highlight, normalize, parseQuery, prepare, search, snippet, stem, tokenize, type SearchDoc, type Segment } from "../src/lib/search.ts";

const doc = (u: string, t: string, x: string, extra: Partial<SearchDoc> = {}): SearchDoc => ({ u, m: "Модуль 1", t, s: "", h: [], x, ...extra });

const docs: SearchDoc[] = [
  doc("/a/", "Горутины", "Горутина — лёгкий поток. Запустить горутину просто: go f()."),
  doc("/b/", "Каналы", "Каналы связывают горутины. Оператор select ждёт несколько каналов.", { h: ["Буферизованные каналы"] }),
  doc("/c/", "errgroup и отмена", "Пакет golang.org/x/sync/errgroup собирает ошибки горутин. Ещё есть sync.Once."),
  doc("/d/", "Ошибки", "Ошибка — это значение. Ещё про ёлки.", { s: "Как устроены ошибки в Go", w: "паникой recover" }),
];
const index = prepare(docs);
const urls = (q: string) => search(index, q).map((h) => h.doc.u);
const plain = (s: Segment[] | null) => (s ?? []).map((p) => (p.mark ? `[${p.text}]` : p.text)).join("");

test("normalize: нижний регистр и ё→е, длина строки та же", () => {
  assert.equal(normalize("Ёлка ЁЖ Sync.Once"), "елка еж sync.once");
  const s = "Ёжик İstanbul";
  assert.equal(normalize(s).length, s.length);
});

test("stem: формы одного слова сводятся к одной основе, латиница не меняется", () => {
  assert.equal(stem("горутины"), "горутин");
  assert.equal(stem("горутина"), "горутин");
  assert.equal(stem("горутин"), "горутин");
  assert.equal(stem("горутинами"), "горутин");
  assert.equal(stem("select"), "select");
  assert.equal(stem("дом"), "дом"); // основа не короче трёх букв
});

test("tokenize: идентификаторы с точкой — целиком и по частям", () => {
  assert.deepEqual(tokenize("Вызов sync.Once."), ["вызов", "sync.once", "sync", "once"]);
  assert.deepEqual(tokenize("errgroup, select{}"), ["errgroup", "select"]);
});

test("parseQuery: повторы и части идентификатора отбрасываются", () => {
  assert.deepEqual(
    parseQuery("sync.Once once Once").map((t) => t.raw),
    ["sync.once"],
  );
  assert.equal(parseQuery("гору")[0].key, "гору"); // короткая основа «гор» нашла бы «город»
  assert.equal(parseQuery("  ").length, 0);
});

test("русское слово в другой форме находит урок", () => {
  assert.equal(urls("горутин")[0], "/a/");
  assert.equal(urls("горутинами")[0], "/a/");
  assert.equal(urls("ошибками")[0], "/d/");
});

test("префикс: недописанное слово тоже ищется", () => {
  assert.deepEqual(urls("горут").slice(0, 1), ["/a/"]);
  assert.ok(urls("канал").includes("/b/"));
});

test("идентификаторы Go находятся как есть", () => {
  assert.deepEqual(urls("errgroup"), ["/c/"]);
  assert.deepEqual(urls("sync.Once"), ["/c/"]);
  assert.deepEqual(urls("select"), ["/b/"]);
});

test("ранжирование: заголовок страницы > заголовок раздела > текст", () => {
  // «горутины» — в заголовке /a/, в тексте /b/ и /c/.
  const r = urls("горутины");
  assert.equal(r[0], "/a/");
  assert.deepEqual(new Set(r.slice(1)), new Set(["/b/", "/c/"]));
  // «буферизованные» — только в заголовке раздела /b/.
  assert.deepEqual(urls("буферизованные"), ["/b/"]);
});

test("несколько слов — все должны встретиться (AND)", () => {
  assert.deepEqual(urls("каналы select"), ["/b/"]);
  assert.deepEqual(urls("каналы errgroup"), []);
  // Слова из w (остаток текста за пределами x) тоже ищутся.
  assert.deepEqual(urls("ошибки паникой"), ["/d/"]);
});

test("ё и е не различаются", () => {
  assert.deepEqual(urls("елки"), ["/d/"]);
  assert.deepEqual(urls("ЁЛКИ"), ["/d/"]);
});

test("сниппет: окно около совпадения, совпадения размечены", () => {
  const text = "Начало. " + "слово ".repeat(60) + "Здесь select ждёт каналы, а дальше " + "текст ".repeat(60);
  const s = snippet(text, "каналы select", 80);
  const out = plain(s);
  assert.ok(out.startsWith("…"), out);
  assert.ok(out.endsWith("…"), out);
  assert.ok(out.includes("[select] ждёт [каналы]"), out);
  assert.equal(snippet(text, "errgroup"), null);
});

test("сниппет: идентификатор с точкой подсвечен целиком, исходный регистр сохранён", () => {
  assert.equal(plain(snippet("Есть Sync.Once и sync.Mutex.", "sync.once")), "Есть [Sync.Once] и sync.Mutex.");
  assert.equal(plain(highlight("Горутины и ЁЛКИ", "горутина елки")), "[Горутины] и [ЁЛКИ]");
});

test("сниппет: разметка из текста остаётся текстом — сегменты, а не HTML", () => {
  const s = snippet("код <script>alert(1)</script> и select", "select")!;
  assert.ok(s.some((p) => !p.mark && p.text.includes("<script>")));
  assert.deepEqual(s.filter((p) => p.mark).map((p) => p.text), ["select"]);
});
