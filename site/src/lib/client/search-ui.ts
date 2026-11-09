// Поле поиска в шапке: индекс грузится при первом фокусе, результаты — выпадающий
// список (ARIA combobox + listbox). Текст из индекса попадает в DOM только
// текстовыми узлами и <mark>, созданными через DOM-API.
import { highlight, prepare, search, snippet, type Prepared, type Segment } from "../search.ts";

const INDEX_URL = "/search-index.json.gz";
const LIMIT = 8;

let index: Promise<Prepared[]> | null = null;

async function fetchIndex(): Promise<Prepared[]> {
  const resp = await fetch(INDEX_URL);
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
  const bytes = new Uint8Array(await resp.arrayBuffer());
  // Сервер, который сам выставил Content-Encoding, отдаст уже распакованный JSON.
  const text =
    bytes[0] === 0x1f && bytes[1] === 0x8b
      ? await new Response(new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"))).text()
      : new TextDecoder().decode(bytes);
  return prepare(JSON.parse(text));
}

function loadIndex(): Promise<Prepared[]> {
  index ??= fetchIndex().catch((e) => {
    index = null; // следующий фокус попробует снова
    throw e;
  });
  return index;
}

function paint(target: HTMLElement, segs: Segment[]) {
  target.replaceChildren(
    ...segs.map((s) => {
      if (!s.mark) return document.createTextNode(s.text);
      const m = document.createElement("mark");
      m.textContent = s.text;
      return m;
    }),
  );
}

function span(className: string) {
  const e = document.createElement("span");
  e.className = className;
  return e;
}

/** Поле ввода в фокусе или редактор — тогда «/» печатается, а не открывает поиск. */
const typing = (t: EventTarget | null) =>
  t instanceof HTMLElement && (t.isContentEditable || !!t.closest("input, textarea, select, .cm-editor"));

export function mountSearch(root: HTMLElement) {
  const input = root.querySelector<HTMLInputElement>("[data-search-input]")!;
  const list = root.querySelector<HTMLUListElement>("[data-search-list]")!;
  const status = root.querySelector<HTMLElement>("[data-search-status]")!;
  const openBtn = root.querySelector<HTMLButtonElement>("[data-search-open]")!;
  const closeBtn = root.querySelector<HTMLButtonElement>("[data-search-close]")!;
  let urls: string[] = [];
  let active = -1;
  let seq = 0;

  // На узком экране поле спрятано за кнопкой-лупой.
  function expand(on: boolean) {
    root.classList.toggle("is-open", on);
    openBtn.setAttribute("aria-expanded", String(on));
    if (on) input.focus();
  }

  function close() {
    list.hidden = true;
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
    active = -1;
  }

  function setActive(i: number) {
    const items = [...list.children] as HTMLElement[];
    if (items.length === 0) return;
    active = (i + items.length) % items.length;
    items.forEach((li, n) => li.setAttribute("aria-selected", String(n === active)));
    input.setAttribute("aria-activedescendant", items[active].id);
    items[active].scrollIntoView({ block: "nearest" });
  }

  function message(text: string) {
    const li = document.createElement("li");
    li.className = "search-empty";
    li.setAttribute("role", "presentation");
    li.textContent = text;
    list.replaceChildren(li);
    list.hidden = false;
    input.setAttribute("aria-expanded", "true");
    status.textContent = text;
    urls = [];
    active = -1;
    input.removeAttribute("aria-activedescendant");
  }

  async function update() {
    const query = input.value.trim();
    const my = ++seq;
    if (!query) {
      close();
      status.textContent = "";
      return;
    }
    let prepared: Prepared[];
    try {
      if (!index) message("Загрузка индекса…");
      prepared = await loadIndex();
    } catch {
      if (my === seq) message("Не удалось загрузить индекс поиска.");
      return;
    }
    if (my !== seq) return; // пока грузился индекс, запрос изменился
    const hits = search(prepared, query, LIMIT);
    if (hits.length === 0) {
      message("Ничего не найдено.");
      return;
    }
    urls = hits.map((h) => h.doc.u);
    list.replaceChildren(
      ...hits.map((h, i) => {
        const li = document.createElement("li");
        li.id = `site-search-opt-${i}`;
        li.setAttribute("role", "option");
        li.setAttribute("aria-selected", "false");
        li.dataset.url = h.doc.u;
        const title = span("search-title");
        paint(title, highlight(h.doc.t, query));
        const where = span("search-where");
        where.textContent = h.doc.m;
        const snip = span("search-snippet");
        paint(snip, snippet(h.doc.x, query) ?? highlight(h.doc.s, query));
        li.append(title, where, snip);
        return li;
      }),
    );
    list.hidden = false;
    input.setAttribute("aria-expanded", "true");
    input.removeAttribute("aria-activedescendant");
    active = -1;
    status.textContent = `Найдено: ${hits.length}${hits.length === LIMIT ? " и больше" : ""}. Стрелки — выбор, Enter — перейти.`;
  }

  const go = (url: string) => location.assign(url);

  input.addEventListener("focus", () => {
    void loadIndex().catch(() => undefined);
    if (input.value.trim()) void update();
  });
  input.addEventListener("input", () => void update());
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      if (list.hidden) {
        void update();
        return;
      }
      e.preventDefault();
      setActive(active < 0 && e.key === "ArrowUp" ? -1 : active + (e.key === "ArrowDown" ? 1 : -1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const url = urls[active >= 0 ? active : 0];
      if (url && !list.hidden) go(url);
    } else if (e.key === "Escape") {
      if (!list.hidden) close();
      else if (input.value) input.value = "";
      else {
        expand(false);
        input.blur();
      }
      e.preventDefault();
    }
  });
  // mousedown не уводит фокус из поля — список не закрывается до клика.
  list.addEventListener("mousedown", (e) => e.preventDefault());
  list.addEventListener("click", (e) => {
    const li = (e.target as HTMLElement).closest<HTMLElement>("[role=option]");
    if (li?.dataset.url) go(li.dataset.url);
  });
  root.addEventListener("focusout", (e) => {
    if (root.contains(e.relatedTarget as Node | null)) return;
    close();
    if (!input.value) expand(false);
  });
  openBtn.addEventListener("click", () => expand(true));
  closeBtn.addEventListener("click", () => {
    input.value = "";
    close();
    expand(false);
    openBtn.focus();
  });

  // «/» с любого места страницы — к поиску.
  document.addEventListener("keydown", (e) => {
    if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented || typing(e.target)) return;
    e.preventDefault();
    expand(true);
    input.select();
  });
}
