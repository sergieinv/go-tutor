// Плеер визуализаций на странице: сценарный режим листает шаги из JSON,
// собранного при сборке; живой — запускает код в runner и строит шаги из трассы.
import type { EditorView } from "@codemirror/view";
import { builders, type LiveBuilder } from "../../../../viz/src/live/index.ts";
import { Player } from "../../../../viz/src/player.ts";
import { createSurface, renderScene, type Surface } from "../../../../viz/src/render.ts";
import type { Step } from "../../../../viz/src/types.ts";
import { race, run, trace, RunnerError, type Files, type Flags } from "./api.ts";
import { createEditor, getDoc, highlightLines, setDoc } from "./editor.ts";
import { loadDraft, saveDraft, storage } from "./progress.ts";
import { showError, showPending, showRun } from "./render.ts";

/** Плееры по корневому элементу — для режима слайдов. */
export const players = new WeakMap<HTMLElement, Player>();

/** `код` в обратных кавычках → <code>, остальное — текстом. */
export function setInline(el: HTMLElement, text: string) {
  el.replaceChildren(
    ...text.split(/(`[^`]+`)/).map((part) => {
      if (part.length > 2 && part.startsWith("`") && part.endsWith("`")) {
        const c = document.createElement("code");
        c.textContent = part.slice(1, -1);
        return c;
      }
      return document.createTextNode(part);
    }),
  );
}

interface Ui {
  root: HTMLElement;
  surface: Surface;
  note: HTMLElement;
  counter: HTMLElement;
  time: HTMLElement;
  seek: HTMLInputElement;
  list: HTMLOListElement;
  btn: (a: string) => HTMLButtonElement;
}

function bindUi(root: HTMLElement, width: number, height: number): Ui {
  const q = <T extends Element>(sel: string) => root.querySelector<T>(sel)!;
  return {
    root,
    surface: createSurface(q<SVGSVGElement>("[data-stage] svg"), width, height),
    note: q("[data-note]"),
    counter: q("[data-counter]"),
    time: q("[data-time]"),
    seek: q("[data-act=seek]"),
    list: q("[data-steps-list]"),
    btn: (a) => q(`[data-act="${a}"]`),
  };
}

function bindPlayer(ui: Ui, player: Player, onLines: (lines: number[]) => void) {
  const { btn } = ui;
  const draw = (p: Player) => {
    const has = p.steps.length > 0;
    ui.root.dataset.ready = String(has);
    for (const a of ["first", "prev", "next", "play"]) btn(a).disabled = !has;
    ui.seek.disabled = !has;
    if (!has) return;
    renderScene(ui.surface, p.step.scene);
    setInline(ui.note, p.step.note);
    ui.counter.textContent = `Шаг ${p.index + 1} из ${p.steps.length}`;
    ui.time.textContent = p.step.time ? `t = ${p.step.time}` : "";
    ui.seek.max = String(p.steps.length - 1);
    ui.seek.value = String(p.index);
    ui.seek.setAttribute("aria-valuetext", ui.counter.textContent);
    btn("first").disabled = btn("prev").disabled = p.atStart;
    btn("next").disabled = p.atEnd;
    btn("play").textContent = p.playing ? "⏸ Пауза" : "▶ Авто";
    btn("play").setAttribute("aria-pressed", String(p.playing));
    onLines(p.step.lines ?? []);
    ui.list.querySelectorAll("li").forEach((li, i) => li.classList.toggle("is-current", i === p.index));
  };
  player.onChange(draw);
  btn("first").addEventListener("click", () => player.go(0));
  btn("prev").addEventListener("click", () => player.prev());
  btn("next").addEventListener("click", () => player.next());
  btn("play").addEventListener("click", () => player.toggle());
  ui.seek.addEventListener("input", () => player.go(Number(ui.seek.value)));
  // ←/→ листают шаги, когда фокус в плеере, но не в редакторе кода.
  ui.root.addEventListener("keydown", (e) => {
    // На краю шагов событие не гасим — режим слайдов перейдёт к соседнему слайду.
    if ((e.target as HTMLElement).closest(".cm-editor, input, select")) return;
    const moved = e.key === "ArrowRight" ? player.next() : e.key === "ArrowLeft" ? player.prev() : false;
    if (moved) e.preventDefault();
  });
  players.set(ui.root, player);
  draw(player);
}

function fillList(list: HTMLOListElement, steps: Step[]) {
  list.replaceChildren(
    ...steps.map((s) => {
      const li = document.createElement("li");
      setInline(li, (s.time ? `[${s.time}] ` : "") + s.note);
      return li;
    }),
  );
}

function mountScenario(root: HTMLElement) {
  const data: { width: number; height: number; steps: Step[] } = JSON.parse(root.querySelector("[data-steps]")!.textContent!);
  const ui = bindUi(root, data.width, data.height);
  const lines = [...root.querySelectorAll<HTMLElement>("[data-code] .line")];
  bindPlayer(ui, new Player(data.steps), (active) => {
    lines.forEach((el, i) => el.classList.toggle("is-active", active.includes(i + 1)));
  });
}

function mountLive(root: HTMLElement) {
  const lesson = root.dataset.lesson!;
  const initial: string = JSON.parse(root.querySelector("[data-live-code]")!.textContent!);
  const host = root.querySelector<HTMLElement>("[data-editor]")!;
  const result = root.querySelector<HTMLElement>("[data-result]")!;
  const procs = root.querySelector<HTMLSelectElement>("[data-gomaxprocs]");
  const runBtn = root.querySelector<HTMLButtonElement>("[data-act=trace]")!;
  const kv = storage();

  const ui = bindUi(root, 680, 200);
  const player = new Player([]);
  host.replaceChildren();
  let busy = false;
  const editor: EditorView = createEditor(host, {
    doc: loadDraft(kv, lesson, "viz") ?? initial,
    label: "Код для живой визуализации",
    onSubmit: () => void build(),
    onChange: (doc) => saveDraft(kv, lesson, "viz", doc, initial),
  });
  bindPlayer(ui, player, (lines) => highlightLines(editor, lines));

  const builder = builders[root.dataset.builder!];

  async function build() {
    if (busy) return;
    busy = true;
    runBtn.disabled = true;
    player.pause();
    const pendingText = { race: "Запуск с детектором гонок…", trace: "Запуск с трассировкой…", run: "Сборка с диагностикой компилятора…" };
    showPending(result, pendingText[builder.endpoint]);
    try {
      const flags: Flags = { ...builder.flags, ...(procs?.value ? { gomaxprocs: Number(procs.value) } : {}) };
      type R = Parameters<typeof showRun>[1];
      const calls: Record<LiveBuilder["endpoint"], (f: Files) => Promise<R>> = {
        race: (f) => race<R>(f, lesson, flags),
        trace: (f) => trace<R>(f, lesson, flags),
        run: (f) => run(f, lesson, flags),
      };
      const r = await calls[builder.endpoint]({ "main.go": getDoc(editor) });
      showRun(result, r, builder.flags?.gcflags);
      const built = builder.build(r);
      if (typeof built === "string") {
        if (!r.buildFailed) {
          const p = document.createElement("p");
          p.className = "status status-bad";
          p.textContent = built;
          result.prepend(p);
        }
        return;
      }
      ui.surface.svg.setAttribute("viewBox", `0 0 ${built.width} ${built.height}`);
      fillList(ui.list, built.steps);
      player.load(built.steps);
    } catch (e) {
      showError(result, e instanceof RunnerError ? e.message : String(e));
    } finally {
      busy = false;
      runBtn.disabled = false;
    }
  }

  runBtn.addEventListener("click", build);
  root.querySelector<HTMLButtonElement>("[data-act=reset]")!.addEventListener("click", () => {
    setDoc(editor, initial);
    highlightLines(editor, []);
  });
  runBtn.disabled = false;
  root.querySelector<HTMLButtonElement>("[data-act=reset]")!.disabled = false;
}

export function mountAll(scope: ParentNode = document) {
  for (const root of scope.querySelectorAll<HTMLElement>("[data-viz]")) {
    if (players.has(root)) continue;
    if (root.dataset.kind === "live") mountLive(root);
    else mountScenario(root);
  }
}
