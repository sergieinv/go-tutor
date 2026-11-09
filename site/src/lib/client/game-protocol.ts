// Протокол WebSocket-шлюза игрового сервера (labs/gameserver, README —
// раздел «Протокол WebSocket»). Здесь только разбор и сборка сообщений:
// без DOM и без состояния, чтобы модуль можно было тестировать в Node.
//
// Всё, что пришло по сети, — недоверенные данные: разбор защитный.
// Неизвестные типы событий пропускаются (сервер может быть новее клиента),
// у известных проверяются типы и размеры полей; событие с битыми полями
// отбрасывается целиком, а не ломает всю пачку.

/** Пачка больше этого размера не разбирается (обычная — единицы КиБ). */
export const MAX_BATCH_BYTES = 4 << 20;
/** Событий в одной пачке не больше этого числа. */
export const MAX_BATCH_EVENTS = 20000;
const MAX_NAME = 32; // ник — 3–16 символов, берём с запасом
const MAX_TEXT = 1024; // чат — до 256 символов
const MAX_BLOCK_NAME = 64;
const MAX_PALETTE = 256;
const MAX_PLACEABLE = 64;
/** Колонок в чанке: 16×16, индекс i = lz*16 + lx. */
export const CHUNK_COLUMNS = 256;

export type PlayerKind = "minecraft" | "web";

export interface WorldInfo {
  chunkSize: number;
  minY: number;
  height: number;
  groundY: number;
  borderRadius: number;
  spawn: { x: number; y: number; z: number };
}

export interface Pos {
  x: number;
  y: number;
  z: number;
  yaw: number;
  pitch: number;
}

export interface WelcomeEvent extends Pos {
  t: "welcome";
  id: number;
  name: string;
  uuid: string;
  tp: number;
  viewDistance: number;
  tickRate: number;
  maxPlayers: number;
  maxChat: number;
  world: WorldInfo;
  placeable: string[];
}
export interface JoinEvent {
  t: "join";
  id: number;
  name: string;
  uuid: string;
  kind: PlayerKind;
}
export interface LeaveEvent {
  t: "leave";
  id: number;
  name: string;
}
export interface SpawnEvent extends Pos {
  t: "spawn";
  id: number;
  name: string;
  kind: PlayerKind;
}
export interface DespawnEvent {
  t: "despawn";
  id: number;
}
export interface MoveEvent extends Pos {
  t: "move";
  id: number;
}
export interface ChatEvent {
  t: "chat";
  id: number;
  name: string;
  text: string;
}
export interface SystemEvent {
  t: "system";
  text: string;
}
export interface CenterEvent {
  t: "center";
  x: number;
  z: number;
}
export interface ChunkEvent {
  t: "chunk";
  x: number;
  z: number;
  palette: string[];
  top: number[];
  height: number[];
}
export interface UnchunkEvent {
  t: "unchunk";
  x: number;
  z: number;
}
export interface BlockEvent {
  t: "block";
  x: number;
  y: number;
  z: number;
  block: string;
  topY: number;
  top: string;
}
export interface TeleportEvent extends Pos {
  t: "teleport";
  tp: number;
  reason: string;
}
export interface KickEvent {
  t: "kick";
  reason: string;
}
export interface TickEvent {
  t: "tick";
  n: number;
  mspt: number;
  tps: number;
  msgs: number;
  bytes: number;
}

export type ServerEvent =
  | WelcomeEvent
  | JoinEvent
  | LeaveEvent
  | SpawnEvent
  | DespawnEvent
  | MoveEvent
  | ChatEvent
  | SystemEvent
  | CenterEvent
  | ChunkEvent
  | UnchunkEvent
  | BlockEvent
  | TeleportEvent
  | KickEvent
  | TickEvent;

export type EventType = ServerEvent["t"];

/** Все типы событий сервера — для переключателей ленты. */
export const EVENT_TYPES: readonly EventType[] = [
  "welcome",
  "join",
  "leave",
  "spawn",
  "despawn",
  "move",
  "chat",
  "system",
  "center",
  "chunk",
  "unchunk",
  "block",
  "teleport",
  "kick",
  "tick",
];

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null && !Array.isArray(v);

// Координаты по модулю не больше 3·10⁷ (граница мира Minecraft) — всё
// остальное считаем мусором, чтобы не рисовать бесконечности.
const LIMIT = 3e7;

function num(o: Obj, k: string): number | undefined {
  const v = o[k];
  return typeof v === "number" && Number.isFinite(v) && Math.abs(v) <= LIMIT ? v : undefined;
}

function int(o: Obj, k: string): number | undefined {
  const v = num(o, k);
  return v !== undefined && Number.isInteger(v) ? v : undefined;
}

function str(o: Obj, k: string, max: number): string | undefined {
  const v = o[k];
  return typeof v === "string" && v.length <= max ? v : undefined;
}

function kind(o: Obj): PlayerKind | undefined {
  const v = o.kind;
  return v === "minecraft" || v === "web" ? v : undefined;
}

/** Позиция с углами: x, y, z обязательны, углы по умолчанию 0. */
function pos(o: Obj): Pos | undefined {
  const x = num(o, "x");
  const y = num(o, "y");
  const z = num(o, "z");
  if (x === undefined || y === undefined || z === undefined) return undefined;
  return { x, y, z, yaw: num(o, "yaw") ?? 0, pitch: num(o, "pitch") ?? 0 };
}

function strings(v: unknown, maxLen: number, maxItem: number): string[] | undefined {
  if (!Array.isArray(v) || v.length > maxLen) return undefined;
  const out: string[] = [];
  for (const s of v) {
    if (typeof s !== "string" || s.length > maxItem) return undefined;
    out.push(s);
  }
  return out;
}

function ints(v: unknown, len: number, min: number, max: number): number[] | undefined {
  if (!Array.isArray(v) || v.length !== len) return undefined;
  for (const n of v) {
    if (typeof n !== "number" || !Number.isInteger(n) || n < min || n > max) return undefined;
  }
  return v as number[];
}

function world(v: unknown): WorldInfo | undefined {
  if (!isObj(v)) return undefined;
  const chunkSize = int(v, "chunkSize");
  const minY = int(v, "minY");
  const height = int(v, "height");
  const groundY = int(v, "groundY");
  const borderRadius = num(v, "borderRadius");
  const sp = isObj(v.spawn) ? v.spawn : undefined;
  const sx = sp && num(sp, "x");
  const sy = sp && num(sp, "y");
  const sz = sp && num(sp, "z");
  if (
    chunkSize !== 16 || // карта и формулы чанков рассчитаны на 16
    minY === undefined ||
    height === undefined ||
    groundY === undefined ||
    borderRadius === undefined ||
    borderRadius <= 0 ||
    sx === undefined ||
    sy === undefined ||
    sz === undefined
  ) {
    return undefined;
  }
  return { chunkSize, minY, height, groundY, borderRadius, spawn: { x: sx, y: sy, z: sz } };
}

/** Одно событие: известный тип с правильными полями или null. */
export function parseEvent(v: unknown): ServerEvent | null {
  if (!isObj(v) || typeof v.t !== "string") return null;
  switch (v.t) {
    case "welcome": {
      const id = int(v, "id");
      const name = str(v, "name", MAX_NAME);
      const p = pos(v);
      const tp = int(v, "tp");
      const viewDistance = int(v, "viewDistance");
      const w = world(v.world);
      const placeable = strings(v.placeable ?? [], MAX_PLACEABLE, MAX_BLOCK_NAME);
      if (id === undefined || name === undefined || !p || tp === undefined || !w || !placeable) return null;
      if (viewDistance === undefined || viewDistance < 1 || viewDistance > 32) return null;
      return {
        t: "welcome",
        id,
        name,
        uuid: str(v, "uuid", 64) ?? "",
        ...p,
        tp,
        viewDistance,
        tickRate: int(v, "tickRate") ?? 20,
        maxPlayers: int(v, "maxPlayers") ?? 0,
        maxChat: int(v, "maxChat") ?? 256,
        world: w,
        placeable,
      };
    }
    case "join": {
      const id = int(v, "id");
      const name = str(v, "name", MAX_NAME);
      if (id === undefined || name === undefined) return null;
      return { t: "join", id, name, uuid: str(v, "uuid", 64) ?? "", kind: kind(v) ?? "web" };
    }
    case "leave": {
      const id = int(v, "id");
      if (id === undefined) return null;
      return { t: "leave", id, name: str(v, "name", MAX_NAME) ?? "" };
    }
    case "spawn": {
      const id = int(v, "id");
      const name = str(v, "name", MAX_NAME);
      const p = pos(v);
      if (id === undefined || name === undefined || !p) return null;
      return { t: "spawn", id, name, kind: kind(v) ?? "web", ...p };
    }
    case "despawn": {
      const id = int(v, "id");
      return id === undefined ? null : { t: "despawn", id };
    }
    case "move": {
      const id = int(v, "id");
      const p = pos(v);
      return id === undefined || !p ? null : { t: "move", id, ...p };
    }
    case "chat": {
      const id = int(v, "id");
      const name = str(v, "name", MAX_NAME);
      const text = str(v, "text", MAX_TEXT);
      if (id === undefined || name === undefined || text === undefined) return null;
      return { t: "chat", id, name, text };
    }
    case "system": {
      const text = str(v, "text", MAX_TEXT);
      return text === undefined ? null : { t: "system", text };
    }
    case "center":
    case "unchunk": {
      const x = int(v, "x");
      const z = int(v, "z");
      if (x === undefined || z === undefined) return null;
      return { t: v.t, x, z };
    }
    case "chunk": {
      const x = int(v, "x");
      const z = int(v, "z");
      const palette = strings(v.palette, MAX_PALETTE, MAX_BLOCK_NAME);
      if (x === undefined || z === undefined || !palette || palette.length === 0) return null;
      const top = ints(v.top, CHUNK_COLUMNS, 0, palette.length - 1);
      const height = ints(v.height, CHUNK_COLUMNS, -4096, 4096);
      if (!top || !height) return null;
      return { t: "chunk", x, z, palette, top, height };
    }
    case "block": {
      const x = int(v, "x");
      const y = int(v, "y");
      const z = int(v, "z");
      const topY = int(v, "topY");
      const block = str(v, "block", MAX_BLOCK_NAME);
      const top = str(v, "top", MAX_BLOCK_NAME);
      if (x === undefined || y === undefined || z === undefined || topY === undefined) return null;
      if (block === undefined || top === undefined) return null;
      return { t: "block", x, y, z, block, topY, top };
    }
    case "teleport": {
      const tp = int(v, "tp");
      const p = pos(v);
      if (tp === undefined || !p) return null;
      return { t: "teleport", tp, ...p, reason: str(v, "reason", 64) ?? "" };
    }
    case "kick":
      return { t: "kick", reason: str(v, "reason", MAX_TEXT) ?? "" };
    case "tick": {
      const n = int(v, "n");
      if (n === undefined || n < 0) return null;
      return {
        t: "tick",
        n,
        mspt: Math.max(0, num(v, "mspt") ?? 0),
        tps: Math.max(0, num(v, "tps") ?? 0),
        msgs: Math.max(0, int(v, "msgs") ?? 0),
        bytes: Math.max(0, int(v, "bytes") ?? 0),
      };
    }
  }
  return null; // неизвестный тип — пропускаем
}

/** Пачка тика: JSON-массив событий. Невалидное сообщение — пустая пачка. */
export function parseBatch(data: unknown): ServerEvent[] {
  if (typeof data !== "string" || data.length > MAX_BATCH_BYTES) return [];
  let raw: unknown;
  try {
    raw = JSON.parse(data);
  } catch {
    return [];
  }
  if (!Array.isArray(raw)) return [];
  const out: ServerEvent[] = [];
  for (let i = 0; i < raw.length && i < MAX_BATCH_EVENTS; i++) {
    const ev = parseEvent(raw[i]);
    if (ev) out.push(ev);
  }
  return out;
}

// --- Клиент → сервер ---

/** Правило ника, как у сервера и Minecraft: 3–16 символов [A-Za-z0-9_]. */
export const validName = (name: string): boolean => /^[A-Za-z0-9_]{3,16}$/.test(name);

const round3 = (v: number) => Math.round(v * 1000) / 1000;

export const helloMsg = (name: string) => JSON.stringify({ t: "hello", name });
export const tpackMsg = (tp: number) => JSON.stringify({ t: "tpack", tp });
export const moveMsg = (x: number, z: number, yaw: number) =>
  JSON.stringify({ t: "move", x: round3(x), z: round3(z), yaw: round3(yaw) });
export const digMsg = (x: number, y: number, z: number) => JSON.stringify({ t: "dig", x, y, z });
export const placeMsg = (x: number, y: number, z: number, block: string) =>
  JSON.stringify({ t: "place", x, y, z, block });

/** Текст чата, как его примет сервер: без управляющих символов и «§», обрезан. */
export function cleanChat(text: string, max = 256): string {
  // eslint-disable-next-line no-control-regex
  const s = text.replace(/[\u0000-\u001f\u007f-\u009f\u2028\u2029§]/g, "").trim();
  return [...s].slice(0, max).join("");
}

export const chatMsg = (text: string) => JSON.stringify({ t: "chat", text });

/** Чанк блока (или точки): 16×16 блоков. */
export const chunkOf = (v: number) => Math.floor(v / 16);
export const chunkKey = (x: number, z: number) => `${x},${z}`;

/** Угол yaw по направлению движения: 0 = +Z (юг), 90 = −X (запад), как в Minecraft. */
export function yawOf(dx: number, dz: number): number {
  const deg = (Math.atan2(-dx, dz) * 180) / Math.PI;
  return deg < 0 ? deg + 360 : deg + 0; // +0 превращает −0 в 0
}
