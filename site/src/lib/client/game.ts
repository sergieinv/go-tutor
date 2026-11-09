// Браузерный клиент игрового сервера (labs/gameserver): сеть, ввод и DOM
// страницы /game/. Протокол — game-protocol.ts, состояние — game-state.ts,
// отрисовка — game-render.ts.
//
// Данные сервера и других игроков попадают в DOM только через textContent:
// ники и чат — чужой ввод.
import {
  chatMsg,
  cleanChat,
  digMsg,
  EVENT_TYPES,
  type EventType,
  helloMsg,
  moveMsg,
  parseBatch,
  placeMsg,
  tpackMsg,
  validName,
  yawOf,
} from "./game-protocol.ts";
import { blockCSS, ChunkImages, drawChart, drawMap, readTheme, type Camera, type Series, type Theme, toWorld } from "./game-render.ts";
import { GameState, HISTORY, step, teleportReason, Throttle } from "./game-state.ts";

export const DEFAULT_URL = "ws://127.0.0.1:25580/ws";
const URL_KEY = "gotutor:game:url";
const NAME_KEY = "gotutor:game:name";
const FILTER_KEY = "gotutor:game:log";

const WALK = 10; // блоков/с (ходьба в Minecraft — 4.3, но на карте это слишком медленно)
const SPRINT = 25; // с Shift; предел сервера — 30
const REACH = 6; // дальность действий с блоками от глаз, как у сервера
const EYE = 1.62;
const LOG_SHOWN = 80;
/** Типы, которые лента показывает по умолчанию (move и tick — каждый тик, шумно). */
const DEFAULT_LOG: EventType[] = ["welcome", "join", "leave", "spawn", "despawn", "center", "chunk", "unchunk", "block", "teleport", "kick", "chat", "system"];

function storageGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function storageSet(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // приватный режим, запрет хранилища — просто не запоминаем
  }
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, text?: string, cls?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  if (cls) e.className = cls;
  return e;
}

export function mountGame(root: HTMLElement) {
  const $ = <T extends Element = HTMLElement>(sel: string) => root.querySelector<T>(sel)!;
  const form = $<HTMLFormElement>("[data-login]");
  const nameInput = $<HTMLInputElement>("[data-name]");
  const urlInput = $<HTMLInputElement>("[data-url]");
  const joinBtn = $<HTMLButtonElement>("[data-join]");
  const leaveBtn = $<HTMLButtonElement>("[data-leave]");
  const statusEl = $("[data-status]");
  const problem = $("[data-problem]");
  const problemText = $("[data-problem-text]");
  const offlineHelp = $("[data-offline-help]");
  const reconnectBtn = $<HTMLButtonElement>("[data-reconnect]");
  const game = $("[data-game]");
  const mapCanvas = $<HTMLCanvasElement>("[data-map]");
  const posEl = $("[data-pos]");
  const hoverEl = $("[data-hover]");
  const tpEl = $("[data-teleport]");
  const toolInputs = [...root.querySelectorAll<HTMLInputElement>("[data-tool]")];
  const blockSelect = $<HTMLSelectElement>("[data-block]");
  const chatLog = $("[data-chat-log]");
  const chatForm = $<HTMLFormElement>("[data-chat-form]");
  const chatInput = $<HTMLInputElement>("[data-chat-input]");
  const chartCanvas = $<HTMLCanvasElement>("[data-chart]");
  const seriesSelect = $<HTMLSelectElement>("[data-series]");
  const rosterEl = $("[data-roster]");
  const visibleEl = $("[data-visible]");
  const rosterCount = $("[data-roster-count]");
  const visibleCount = $("[data-visible-count]");
  const logEl = $("[data-log]");
  const logFilters = $("[data-log-filters]");
  const logPause = $<HTMLInputElement>("[data-log-pause]");
  const stat = (k: string) => $(`[data-stat="${k}"]`);

  urlInput.value = storageGet(URL_KEY) || DEFAULT_URL;
  nameInput.value = storageGet(NAME_KEY) || `Gopher${Math.floor(100 + Math.random() * 900)}`;

  // Переключатели ленты событий по типам.
  let shownTypes = new Set<EventType>(DEFAULT_LOG);
  try {
    const saved = JSON.parse(storageGet(FILTER_KEY) ?? "null");
    if (Array.isArray(saved)) shownTypes = new Set(saved.filter((t): t is EventType => EVENT_TYPES.includes(t)));
  } catch {
    // битое значение — оставляем по умолчанию
  }
  const filterCounts = new Map<EventType, HTMLElement>();
  for (const t of EVENT_TYPES) {
    const label = el("label", undefined, "log-filter");
    const box = el("input");
    box.type = "checkbox";
    box.checked = shownTypes.has(t);
    box.dataset.type = t;
    box.addEventListener("change", () => {
      if (box.checked) shownTypes.add(t);
      else shownTypes.delete(t);
      storageSet(FILTER_KEY, JSON.stringify([...shownTypes]));
      logDirty = true;
    });
    const count = el("span", "0", "log-count");
    filterCounts.set(t, count);
    label.append(box, el("code", t), count);
    logFilters.append(label);
  }

  let theme: Theme = readTheme(root);
  matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    theme = readTheme(root);
  });

  let ws: WebSocket | null = null;
  let state = new GameState();
  let images = new ChunkImages();
  let opened = false;
  let leaving = false;
  let cam: Camera | null = null;
  let target: { x: number; z: number } | null = null;
  let hover: { x: number; z: number } | null = null;
  let moveDirty = false;
  let lastFrame = 0;
  let raf = 0;
  let uiDirty = true;
  let logDirty = true;
  let chartDirty = true;
  let renderedChat: unknown = null;
  let renderedChatLen = -1;
  const moveThrottle = new Throttle(55); // сервер — раз в тик (50 мс); с запасом на дрожание таймера
  const blockThrottle = new Throttle(120); // сервер — 10 действий/с
  const keys = new Set<string>();

  function setStatus(text: string, kind: "ok" | "bad" | "warn" | "" = "") {
    statusEl.textContent = text;
    statusEl.className = `status${kind ? ` status-${kind}` : ""}`;
  }

  function showProblem(text: string, offline: boolean) {
    problemText.textContent = text;
    offlineHelp.hidden = !offline;
    problem.hidden = false;
  }

  function send(msg: string) {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(msg);
  }

  function connect() {
    const name = nameInput.value.trim();
    if (!validName(name)) {
      setStatus("Имя — 3–16 символов: латинские буквы, цифры и _.", "bad");
      nameInput.focus();
      return;
    }
    const url = urlInput.value.trim() || DEFAULT_URL;
    storageSet(URL_KEY, url);
    storageSet(NAME_KEY, name);
    disconnect();
    state = new GameState();
    images = new ChunkImages();
    opened = false;
    leaving = false;
    target = null;
    renderedChat = null;
    problem.hidden = true;
    try {
      ws = new WebSocket(url);
    } catch {
      setStatus(`Неверный адрес WebSocket: ${url}`, "bad");
      return;
    }
    setStatus(`Подключение к ${url}…`, "warn");
    joinBtn.disabled = true;
    const sock = ws;
    sock.onopen = () => {
      opened = true;
      setStatus("Соединение открыто, вход…", "warn");
      sock.send(helloMsg(name));
    };
    sock.onmessage = (e) => {
      if (sock !== ws) return;
      const hadMe = state.me !== null;
      const fx = state.apply(parseBatch(e.data));
      for (const tp of fx.tpacks) {
        sock.send(tpackMsg(tp));
        state.acked(tp);
      }
      if (fx.repositioned) moveDirty = false;
      if (!hadMe && state.me) {
        setStatus(`В игре: ${state.me.name} (id ${state.me.id})`, "ok");
        game.hidden = false;
        leaveBtn.hidden = false;
        fillBlocks();
        start();
      }
      uiDirty = logDirty = chartDirty = true;
    };
    sock.onclose = (e) => {
      if (sock !== ws) return;
      ws = null;
      joinBtn.disabled = false;
      leaveBtn.hidden = true;
      stop();
      render(); // последний кадр — с причиной
      if (leaving) {
        setStatus("Вы вышли из игры.", "");
        return;
      }
      if (!opened) {
        setStatus("Сервер недоступен.", "bad");
        showProblem(
          `Не удалось подключиться к ${url}. Сервер не запущен, адрес неверный или страница открыта не с разрешённого адреса (Origin: порты 8080 и 4321).`,
          true,
        );
        return;
      }
      if (state.kicked !== null) {
        setStatus("Сервер отключил клиента.", "bad");
        showProblem(`Причина: ${state.kicked || "не указана"} (код закрытия ${e.code}).`, false);
      } else if (e.code === 1001) {
        setStatus("Сервер останавливается.", "bad");
        showProblem("Сервер закрыл соединение: остановка (код 1001).", false);
      } else {
        setStatus("Соединение закрыто.", "bad");
        showProblem(`Соединение закрыто (код ${e.code}${e.reason ? `, ${e.reason}` : ""}).`, e.code === 1006 && !state.me);
      }
    };
  }

  function disconnect() {
    if (ws) {
      const old = ws;
      ws = null;
      old.onclose = old.onmessage = old.onopen = null;
      old.close(1000);
    }
    stop();
  }

  form.addEventListener("submit", (e) => {
    e.preventDefault();
    connect();
  });
  reconnectBtn.addEventListener("click", connect);
  leaveBtn.addEventListener("click", () => {
    leaving = true;
    ws?.close(1000);
  });

  function fillBlocks() {
    blockSelect.replaceChildren();
    for (const b of state.placeable) {
      const o = el("option", b.replace("minecraft:", ""));
      o.value = b;
      o.style.color = blockCSS(b);
      blockSelect.append(o);
    }
  }

  // --- Ввод ---

  const MOVE_KEYS: Record<string, [number, number]> = {
    KeyW: [0, -1],
    ArrowUp: [0, -1],
    KeyS: [0, 1],
    ArrowDown: [0, 1],
    KeyA: [-1, 0],
    ArrowLeft: [-1, 0],
    KeyD: [1, 0],
    ArrowRight: [1, 0],
  };

  const typing = (t: EventTarget | null) =>
    t instanceof HTMLElement && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName)) && !(t instanceof HTMLInputElement && (t.type === "radio" || t.type === "checkbox"));

  document.addEventListener("keydown", (e) => {
    if (!state.me || !ws || typing(e.target) || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.code in MOVE_KEYS) {
      keys.add(e.code);
      target = null;
      e.preventDefault();
    }
    if (e.key === "Shift") keys.add("Shift");
  });
  document.addEventListener("keyup", (e) => {
    keys.delete(e.code);
    if (e.key === "Shift") keys.delete("Shift");
  });
  window.addEventListener("blur", () => keys.clear());

  const tool = () => toolInputs.find((i) => i.checked)?.value ?? "walk";

  function worldAt(e: MouseEvent): { x: number; z: number } | null {
    if (!cam) return null;
    const r = mapCanvas.getBoundingClientRect();
    const [x, z] = toWorld(cam, e.clientX - r.left, e.clientY - r.top);
    return { x, z };
  }

  mapCanvas.addEventListener("click", (e) => {
    const p = worldAt(e);
    if (!p || !state.me || !ws) return;
    const t = tool();
    if (t === "walk") {
      const b = (state.world?.borderRadius ?? Infinity) - 0.5;
      target = { x: Math.max(-b, Math.min(b, p.x)), z: Math.max(-b, Math.min(b, p.z)) };
      return;
    }
    const top = state.topAt(p.x, p.z);
    if (!top) return;
    const bx = Math.floor(p.x);
    const bz = Math.floor(p.z);
    const by = t === "dig" ? top.y : top.y + 1;
    const dist = Math.hypot(bx + 0.5 - state.me.x, by + 0.5 - (state.me.y + EYE), bz + 0.5 - state.me.z);
    if (dist > REACH) {
      tpEl.textContent = `Блок далеко (${dist.toFixed(1)} > ${REACH}): подойдите ближе.`;
      return;
    }
    if (!blockThrottle.take(performance.now())) return;
    send(t === "dig" ? digMsg(bx, by, bz) : placeMsg(bx, by, bz, blockSelect.value));
  });
  mapCanvas.addEventListener("mousemove", (e) => {
    hover = worldAt(e);
    uiDirty = true;
  });
  mapCanvas.addEventListener("mouseleave", () => {
    hover = null;
    uiDirty = true;
  });

  chatForm.addEventListener("submit", (e) => {
    e.preventDefault();
    const text = cleanChat(chatInput.value, state.maxChat);
    if (!text || !ws) return;
    send(chatMsg(text));
    chatInput.value = "";
  });

  seriesSelect.addEventListener("change", () => (chartDirty = true));

  // --- Цикл кадра ---

  function start() {
    if (!raf) {
      lastFrame = performance.now();
      raf = requestAnimationFrame(frame);
    }
  }

  function stop() {
    if (raf) cancelAnimationFrame(raf);
    raf = 0;
    keys.clear();
  }

  function frame(now: number) {
    raf = requestAnimationFrame(frame);
    const dt = (now - lastFrame) / 1000;
    lastFrame = now;
    moveLocal(dt, now);
    render();
  }

  function moveLocal(dt: number, now: number) {
    const me = state.me;
    if (!me || !state.world || !state.canMove()) return;
    let dx = 0;
    let dz = 0;
    for (const k of keys) {
      const d = MOVE_KEYS[k];
      if (d) {
        dx += d[0];
        dz += d[1];
      }
    }
    if (dx === 0 && dz === 0 && target) {
      dx = target.x - me.x;
      dz = target.z - me.z;
      if (Math.hypot(dx, dz) < 0.2) {
        target = null;
        dx = dz = 0;
      }
    }
    if (dx !== 0 || dz !== 0) {
      const speed = keys.has("Shift") ? SPRINT : WALK;
      let next = step(me, dx, dz, speed, dt, state.world.borderRadius);
      // К цели — не дальше самой цели.
      if (target && Math.hypot(target.x - me.x, target.z - me.z) <= Math.hypot(next.x - me.x, next.z - me.z)) {
        next = { x: target.x, z: target.z };
        target = null;
      }
      me.yaw = yawOf(dx, dz);
      me.x = next.x;
      me.z = next.z;
      moveDirty = true;
      uiDirty = true;
    }
    if (moveDirty && moveThrottle.take(now)) {
      send(moveMsg(me.x, me.z, me.yaw));
      moveDirty = false;
    }
  }

  function render() {
    cam = drawMap(mapCanvas, state, images, theme, { target, hover });
    if (chartDirty) {
      drawChart(chartCanvas, state.ticks, seriesSelect.value as Series, theme, HISTORY);
      chartDirty = false;
    }
    if (uiDirty) {
      paintPanels();
      uiDirty = false;
    }
    if (logDirty && !logPause.checked) {
      paintLog();
      logDirty = false;
    }
  }

  const fmt = (v: number, d = 1) => (Math.round(v * 10 ** d) / 10 ** d).toString();

  function paintPanels() {
    const me = state.me;
    if (me) {
      const cx = Math.floor(me.x / 16);
      const cz = Math.floor(me.z / 16);
      posEl.textContent = `x ${fmt(me.x)}, z ${fmt(me.z)} · чанк ${cx}, ${cz} · yaw ${Math.round(me.yaw)}°${state.pendingTp !== null ? ` · ждём tpack ${state.pendingTp}` : ""}`;
    }
    if (hover) {
      const top = state.topAt(hover.x, hover.z);
      const bx = Math.floor(hover.x);
      const bz = Math.floor(hover.z);
      hoverEl.textContent = top
        ? `Под курсором: ${bx}, ${bz} · чанк ${Math.floor(bx / 16)}, ${Math.floor(bz / 16)} · ${top.block.replace("minecraft:", "")} (y ${top.y})`
        : `Под курсором: ${bx}, ${bz} · чанк не загружен`;
    } else {
      hoverEl.textContent = "Наведите курсор на карту.";
    }
    if (state.lastTeleport && state.lastTeleport.reason !== "resend") {
      tpEl.textContent = `Поправка сервера (тик ${state.lastTeleport.tick}): ${teleportReason(state.lastTeleport.reason)}.`;
    }

    // Статистика последнего тика.
    const t = state.ticks[state.ticks.length - 1];
    stat("tick").textContent = t ? String(t.n) : "—";
    stat("mspt").textContent = t ? t.mspt.toFixed(3) : "—";
    stat("tps").textContent = t ? fmt(t.tps, 2) : "—";
    stat("msgs").textContent = t ? String(t.msgs) : "—";
    stat("bytes").textContent = t ? String(t.bytes) : "—";
    const zone = (2 * state.viewDistance + 1) ** 2;
    stat("chunks").textContent = `${state.chunks.size} / ${zone}`;
    stat("center").textContent = state.center ? `${state.center.x}, ${state.center.z}` : "—";

    // Interest management: все онлайн против видимых.
    rosterCount.textContent = String(state.roster.size);
    visibleCount.textContent = String(state.visible.size);
    const roster = [...state.roster.values()].sort((a, b) => a.name.localeCompare(b.name));
    rosterEl.replaceChildren(
      ...roster.map((p) => {
        const li = el("li");
        const self = p.id === me?.id;
        const seen = state.visible.has(p.id);
        li.dataset.name = p.name;
        li.dataset.state = self ? "self" : seen ? "visible" : "hidden";
        li.append(el("span", "", "dot"), el("span", p.name, "pname"), el("span", self ? "вы" : seen ? "в зоне" : "вне зоны", "muted tag"));
        if (p.kind === "minecraft") li.append(el("span", "MC", "badge"));
        return li;
      }),
    );
    const visible = [...state.visible.values()].sort((a, b) => a.name.localeCompare(b.name));
    visibleEl.replaceChildren(
      ...visible.map((p) => {
        const li = el("li");
        li.dataset.name = p.name;
        li.dataset.state = "visible";
        const d = me ? Math.hypot(p.x - me.x, p.z - me.z) : 0;
        li.append(el("span", "", "dot"), el("span", p.name, "pname"), el("span", `${Math.round(d)} бл.`, "muted tag"));
        return li;
      }),
    );
    if (visible.length === 0) visibleEl.append(el("li", "никого — подойдите к другим игрокам", "muted empty"));

    for (const [ty, node] of filterCounts) node.textContent = String(state.counts.get(ty) ?? 0);

    // Чат: перерисовываем, только если пришли новые строки.
    const last = state.chat[state.chat.length - 1];
    if (last !== renderedChat || state.chat.length !== renderedChatLen) {
      const atBottom = chatLog.scrollHeight - chatLog.scrollTop - chatLog.clientHeight < 24;
      chatLog.replaceChildren(
        ...state.chat.map((c) => {
          const li = el("li", undefined, `chat-${c.kind}${c.self ? " chat-self" : ""}`);
          if (c.kind === "chat") li.append(el("b", `<${c.name}> `), document.createTextNode(c.text));
          else li.textContent = c.text;
          return li;
        }),
      );
      if (atBottom || renderedChat === null) chatLog.scrollTop = chatLog.scrollHeight;
      renderedChat = last;
      renderedChatLen = state.chat.length;
    }
    chatInput.maxLength = state.maxChat;
  }

  function paintLog() {
    const items = [];
    for (const e of state.recent(shownTypes, LOG_SHOWN)) {
      const li = el("li", undefined, `ev ev-${e.t}`);
      li.append(el("span", String(e.tick), "ev-tick"), el("code", e.t, "ev-type"), el("span", e.text, "ev-text"));
      items.push(li);
    }
    if (shownTypes.has("tick")) {
      // tick в журнал не пишется (каждые 50 мс) — показываем последние из истории.
      for (const t of state.ticks.slice(-5).reverse()) {
        const li = el("li", undefined, "ev ev-tick-row");
        li.append(el("span", String(t.n), "ev-tick"), el("code", "tick", "ev-type"), el("span", `mspt ${t.mspt} · tps ${t.tps} · msgs ${t.msgs} · bytes ${t.bytes}`, "ev-text"));
        items.unshift(li);
      }
    }
    if (items.length === 0) items.push(el("li", "событий выбранных типов пока нет", "muted empty"));
    logEl.replaceChildren(...items);
  }

  // Первый кадр — пустая карта, чтобы canvas не был прозрачным.
  window.addEventListener("resize", () => {
    if (!raf) render();
    chartDirty = true;
  });
  render();
}
