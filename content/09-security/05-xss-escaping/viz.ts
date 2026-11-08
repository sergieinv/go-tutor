// Сценарий «Под капотом»: экранировщик html/template проходит по разметке
// шаблона, отслеживает контекст и дописывает в каждое действие свои функции.
// Одно значение в разных контекстах превращается в разные строки, а
// template.HTML проходит без изменений.
import { box, defineScenario, scene, struct } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `type Profile struct {
	Name, Site string
	Bio        template.HTML
}

const page = \`<p>{{.Name}}</p>
<a title="{{.Name}}"
   href="/u?name={{.Name}}">профиль</a>
<a href="{{.Site}}">сайт</a>
<script>let user = {{.Name}};</script>
<div>{{.Bio}}</div>\`

t := template.Must(template.New("p").Parse(page))
t.Execute(w, Profile{
	Name: \`Tom & "Jerry"\`,
	Site: "javascript:alert(1)",
	Bio:  template.HTML(\`<img src=x onerror=alert(1)>\`),
})
t.Execute(w, Profile{Name: "</script><b>"})`;

const tplLines = [
  "<p>{{.Name}}</p>",
  '<a title="{{.Name}}"',
  // Пробелы в начале строки SVG схлопывает — отступ неразрывными.
  '\u00a0\u00a0\u00a0href="/u?name={{.Name}}">профиль</a>',
  '<a href="{{.Site}}">сайт</a>',
  "<script>let user = {{.Name}};</script>",
  "<div>{{.Bio}}</div>",
];

/** Строка результата: контекст и то, во что превратилось значение. */
interface Row {
  ctx: string;
  out: string;
  /** Пометка справа от значения. */
  mark?: string;
}

interface Frame {
  /** Индекс строки шаблона, где стоит экранировщик (0…5). */
  at?: number;
  ctx: [state: string, delim: string, urlPart: string, element: string];
  ctxHl?: string[];
  pipeTitle: string;
  pipe: string[];
  rows: (Row | undefined)[];
  /** Подсвеченная строка результата. */
  rowHl?: number;
}

const X = 10;
const W = 620;
const TPL_Y = 10;
const LINE = 18;
const RES_Y = 312;
const LABEL_X = 22;
const VALUE_X = 120;

function frame(f: Frame) {
  const parts: Parameters<typeof scene> = [];

  // Шаблон: подсветка строки встаёт между рамкой и текстом.
  const tpl = box("tpl", { x: X, y: TPL_Y, w: W, h: 38 + (tplLines.length - 1) * LINE + 14, title: "шаблон page — разметка, которую читает экранировщик", lines: tplLines });
  const tplShapes = [...tpl.shapes];
  if (f.at !== undefined) {
    tplShapes.splice(1, 0, { type: "rect", id: "tpl:hl", x: X + 4, y: TPL_Y + 38 + f.at * LINE - 13, w: W - 8, h: LINE, rx: 3, cls: "v-hl" });
  }
  parts.push(tplShapes);

  parts.push(
    struct("ctx", {
      x: X,
      y: 168,
      w: 300,
      title: "контекст экранировщика",
      fields: [
        ["state", f.ctx[0]],
        ["delim", f.ctx[1]],
        ["urlPart", f.ctx[2]],
        ["element", f.ctx[3]],
      ],
      hl: f.ctxHl,
    }),
  );
  parts.push(box("pipe", { x: 325, y: 168, w: 305, h: 130, title: f.pipeTitle, lines: f.pipe }));

  // Таблица «контекст → вывод»: подписи и значения — отдельные тексты,
  // потому что пробелы внутри SVG-текста схлопываются.
  const rows = f.rows;
  const resH = 38 + (rows.length - 1) * LINE + 14;
  const res: Shape[] = [
    { type: "rect", id: "res:box", x: X, y: RES_Y, w: W, h: resH, rx: 8, cls: "v-box" },
    { type: "text", id: "res:title", x: X + 10, y: RES_Y + 18, text: "значение → вывод", cls: "v-struct-title" },
  ];
  if (f.rowHl !== undefined) {
    res.push({ type: "rect", id: "res:hl", x: X + 4, y: RES_Y + 38 + f.rowHl * LINE - 13, w: W - 8, h: LINE, rx: 3, cls: "v-hl" });
  }
  rows.forEach((r, i) => {
    if (!r) return;
    const y = RES_Y + 38 + i * LINE;
    res.push({ type: "text", id: `res:ctx${i}`, x: LABEL_X, y, text: r.ctx, cls: "v-field" });
    res.push({ type: "text", id: `res:out${i}`, x: VALUE_X, y, text: r.out, cls: "v-box-line" });
    if (r.mark) res.push({ type: "text", id: `res:mark${i}`, x: X + W - 12, y, text: r.mark, cls: "v-wait", anchor: "end" });
  });
  parts.push(res);
  return scene(...parts);
}

const none: Frame["ctx"] = ["—", "—", "—", "—"];

// Результаты первого Execute по строкам шаблона.
const r1: Row[] = [
  { ctx: "текст", out: "Tom &amp; &#34;Jerry&#34;" },
  { ctx: "атрибут", out: "Tom &amp; &#34;Jerry&#34;" },
  { ctx: "URL ?name=", out: "Tom%20%26%20%22Jerry%22" },
  { ctx: "href=", out: "#ZgotmplZ", mark: "javascript: отброшен" },
  { ctx: "<script>", out: '"Tom \\u0026 \\"Jerry\\""' },
  { ctx: "HTML", out: "<img src=x onerror=alert(1)>", mark: "как есть — XSS" },
];
const upTo = (n: number): (Row | undefined)[] => r1.map((r, i) => (i < n ? r : undefined));

export default defineScenario({
  title: "Контекстное экранирование html/template",
  code,
  width: 640,
  height: 470,
  steps: [
    {
      lines: [13],
      note: "`Parse` в `html/template` строит то же дерево, что и `text/template`: действия `{{.Name}}` пока без экранирования. Анализ разметки откладывается до первого `Execute` — до него шаблон ещё можно дополнять (`{{define}}`, `AddParseTree`).",
      scene: frame({ ctx: none, pipeTitle: "действия", pipe: ["экранировщиков ещё нет:", "дерево как у text/template"], rows: upTo(0) }),
    },
    {
      lines: [14, 15, 6],
      note: "Первый `Execute` запускает экранировщик. Он идёт по тексту шаблона как HTML-парсер, начиная с состояния `stateText`. Первое действие стоит в тексте между тегами — в него дописывается `_html_template_htmlescaper`: `&`, `<`, `>`, кавычки → сущности.",
      scene: frame({
        at: 0,
        ctx: ["stateText", "delimNone", "urlPartNone", "elementNone"],
        ctxHl: ["state"],
        pipeTitle: "{{.Name}} в тексте",
        pipe: ["{{.Name", "| _html_template_htmlescaper}}"],
        rows: upTo(1),
        rowHl: 0,
      }),
    },
    {
      lines: [7],
      note: "Внутри тега `<a` парсер встречает атрибут `title=\"` — состояние `stateAttr`, значение ограничено двойной кавычкой. Экранировщик атрибута кодирует кавычки, иначе `\"` из данных закрыл бы значение и открыл новый атрибут вроде `onmouseover=`.",
      scene: frame({
        at: 1,
        ctx: ["stateAttr", "delimDoubleQuote", "urlPartNone", "elementNone"],
        ctxHl: ["state", "delim"],
        pipeTitle: "{{.Name}} в title=\"…\"",
        pipe: ["{{.Name", "| _html_template_attrescaper}}"],
        rows: upTo(2),
        rowHl: 1,
      }),
    },
    {
      lines: [8],
      note: "Атрибут `href` содержит URL: состояние `stateURL`. Действие стоит после `?` — это часть запроса (`urlPartQueryOrFrag`), значит, сначала процентное кодирование (`&` → `%26`, пробел → `%20`), потом — экранирование для атрибута.",
      scene: frame({
        at: 2,
        ctx: ["stateURL", "delimDoubleQuote", "urlPartQueryOrFrag", "elementNone"],
        ctxHl: ["state", "urlPart"],
        pipeTitle: "{{.Name}} в ?name=…",
        pipe: ["{{.Name", "| _html_template_urlescaper", "| _html_template_attrescaper}}"],
        rows: upTo(3),
        rowHl: 2,
      }),
    },
    {
      lines: [9, 16],
      note: "Здесь значение стоит в начале URL и задаёт схему (`urlPartNone`). Добавляется `urlfilter`: схемы, кроме `http`, `https` и `mailto`, он заменяет на `#ZgotmplZ` — ссылка `javascript:alert(1)` превращается в безвредный якорь.",
      scene: frame({
        at: 3,
        ctx: ["stateURL", "delimDoubleQuote", "urlPartNone", "elementNone"],
        ctxHl: ["urlPart"],
        pipeTitle: "{{.Site}} в начале href",
        pipe: ["{{.Site", "| _html_template_urlfilter", "| _html_template_urlnormalizer", "| _html_template_attrescaper}}"],
        rows: upTo(4),
        rowHl: 3,
      }),
    },
    {
      lines: [10],
      note: "После `<script>` парсер в состоянии `stateJS`. HTML-сущности браузер внутри скрипта не раскодирует, поэтому здесь другой экранировщик: `jsvalescaper` кодирует значение в JSON и заменяет `<`, `>`, `&` на `\\u003c`, `\\u003e`, `\\u0026`.",
      scene: frame({
        at: 4,
        ctx: ["stateJS", "delimNone", "urlPartNone", "elementScript"],
        ctxHl: ["state", "element"],
        pipeTitle: "{{.Name}} в <script>",
        pipe: ["{{.Name", "| _html_template_jsvalescaper}}"],
        rows: upTo(5),
        rowHl: 4,
      }),
    },
    {
      lines: [11, 17],
      note: "`{{.Bio}}` снова в тексте и получает тот же `htmlescaper`. Но значение имеет тип `template.HTML` — экранировщик проверяет тип и возвращает строку без изменений. Атрибут `onerror` выполнится у каждого, кто откроет страницу: приведение к `template.HTML` отключает защиту.",
      scene: frame({
        at: 5,
        ctx: ["stateText", "delimNone", "urlPartNone", "elementNone"],
        ctxHl: ["state"],
        pipeTitle: "{{.Bio}} в тексте",
        pipe: ["{{.Bio", "| _html_template_htmlescaper}}", "тип template.HTML —", "строка проходит как есть"],
        rows: upTo(6),
        rowHl: 5,
      }),
    },
    {
      lines: [14, 15, 16, 17, 18],
      note: "Шаблон закончился в `stateText` — иначе `Execute` вернул бы ошибку «ends in a non-text context». Переписанное дерево сохраняется в `t`: анализ делается один раз, а `Parse` после `Execute` возвращает ошибку `cannot Parse after Execute`.",
      scene: frame({
        ctx: ["stateText", "delimNone", "urlPartNone", "elementNone"],
        pipeTitle: "итог анализа",
        pipe: ["6 действий переписаны", "дерево сохранено в t", "повторного анализа не будет"],
        rows: upTo(6),
      }),
    },
    {
      lines: [19],
      note: "Второй `Execute` берёт готовые конвейеры, меняются только данные. `</script><b>` в `<script>` становится `\"\\u003c/script\\u003e\\u003cb\\u003e\"`: браузер не увидит закрывающий тег, а для JavaScript это та же строка. Пустой `Site` даёт `href=\"\"`.",
      scene: frame({
        at: 4,
        ctx: ["stateJS", "delimNone", "urlPartNone", "elementScript"],
        pipeTitle: "{{.Name}} в <script>",
        pipe: ["{{.Name", "| _html_template_jsvalescaper}}"],
        rows: [
          { ctx: "текст", out: "&lt;/script&gt;&lt;b&gt;" },
          { ctx: "атрибут", out: "&lt;/script&gt;&lt;b&gt;" },
          { ctx: "URL ?name=", out: "%3c%2fscript%3e%3cb%3e" },
          { ctx: "href=", out: "(пусто)" },
          { ctx: "<script>", out: '"\\u003c/script\\u003e\\u003cb\\u003e"' },
          { ctx: "HTML", out: "(пусто)" },
        ],
        rowHl: 4,
      }),
    },
  ],
});
