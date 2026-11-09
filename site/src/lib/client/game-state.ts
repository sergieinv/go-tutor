// Состояние браузерного клиента игрового сервера: что клиент знает о мире
// по событиям шлюза. Без DOM и сети — применяется пачка событий тика,
// наружу отдаются только исходящие подтверждения (tpack). Отрисовка — в
// game-render.ts, сеть и DOM — в game.ts.
import {
  chunkKey,
  type ChunkEvent,
  type EventType,
  type PlayerKind,
  type Pos,
  type ServerEvent,
  type TickEvent,
  type WorldInfo,
} from "./game-protocol.ts";

/** Тиков в истории для графиков визуализатора (5 с при 20 TPS). */
export const HISTORY = 100;
/** Сколько тиков «вспыхивает» чанк после загрузки/выгрузки. */
export const FLASH_TICKS = 10;
/** Записей ленты на каждый тип: частые move не вытесняют редкие spawn. */
const MAX_LOG = 100;
const MAX_CHAT = 200;

export interface Me extends Pos {
  id: number;
  name: string;
}

export interface RosterEntry {
  id: number;
  name: string;
  kind: PlayerKind;
}

export interface Entity extends Pos {
  id: number;
  name: string;
  kind: PlayerKind;
}

/** Чанк карты сверху. palette дополняется при изменениях блоков. */
export interface Chunk {
  x: number;
  z: number;
  palette: string[];
  top: number[];
  height: number[];
  /** Растёт при каждом изменении — по нему отрисовщик обновляет кеш. */
  version: number;
}

export interface Flash {
  x: number;
  z: number;
  kind: "load" | "unload";
  tick: number;
}

export interface LogEntry {
  /** Сквозной номер — порядок событий между типами. */
  seq: number;
  tick: number;
  t: EventType;
  text: string;
}

export interface ChatLine {
  kind: "chat" | "system" | "join" | "leave" | "teleport" | "kick";
  name?: string;
  text: string;
  self?: boolean;
}

export interface TickStat {
  n: number;
  mspt: number;
  tps: number;
  msgs: number;
  bytes: number;
  /** Чанков загружено / выгружено этому клиенту в этом тике. */
  loaded: number;
  unloaded: number;
}

/** Что сделать после применения пачки. */
export interface Effects {
  /** Номера телепортов, на которые надо ответить tpack. */
  tpacks: number[];
  /** Позиция игрока поставлена сервером (welcome / teleport). */
  repositioned: boolean;
  kicked?: string;
}

const short = (v: number) => (Math.round(v * 10) / 10).toString();

/** Короткое описание события для ленты визуализатора. */
export function describe(ev: ServerEvent): string {
  switch (ev.t) {
    case "welcome":
      return `id ${ev.id} ${ev.name} @ ${short(ev.x)}, ${short(ev.z)}, tp ${ev.tp}, R=${ev.viewDistance}`;
    case "join":
      return `${ev.name} (id ${ev.id}, ${ev.kind})`;
    case "leave":
      return `${ev.name} (id ${ev.id})`;
    case "spawn":
      return `${ev.name} (id ${ev.id}) @ ${short(ev.x)}, ${short(ev.z)}`;
    case "despawn":
      return `id ${ev.id}`;
    case "move":
      return `id ${ev.id} → ${short(ev.x)}, ${short(ev.z)}`;
    case "chat":
      return `<${ev.name}> ${ev.text}`;
    case "system":
      return ev.text;
    case "center":
    case "unchunk":
      return `чанк ${ev.x}, ${ev.z}`;
    case "chunk":
      return `чанк ${ev.x}, ${ev.z}, палитра: ${ev.palette.length}`;
    case "block":
      return `${ev.x}, ${ev.y}, ${ev.z} = ${ev.block.replace("minecraft:", "")}`;
    case "teleport":
      return `tp ${ev.tp} → ${short(ev.x)}, ${short(ev.z)} (${ev.reason})`;
    case "kick":
      return ev.reason;
    case "tick":
      return `#${ev.n} mspt ${ev.mspt} tps ${ev.tps} msgs ${ev.msgs} bytes ${ev.bytes}`;
  }
}

/** Пояснение причины поправки позиции сервером. */
export function teleportReason(reason: string): string {
  switch (reason) {
    case "too_fast":
      return "слишком быстро — сервер вернул назад";
    case "out_of_bounds":
      return "за границей мира — сервер вернул в мир";
    case "resend":
      return "нет подтверждения tpack — сервер повторил телепорт";
    case "":
      return "сервер поставил позицию";
    default:
      return reason;
  }
}

function pushCapped<T>(arr: T[], v: T, cap: number) {
  arr.push(v);
  if (arr.length > cap) arr.splice(0, arr.length - cap);
}

export class GameState {
  me: Me | null = null;
  world: WorldInfo | null = null;
  viewDistance = 6;
  maxChat = 256;
  placeable: string[] = [];
  /** Последний телепорт, ещё не подтверждённый (пока он есть, движение игнорируется сервером). */
  pendingTp: number | null = null;
  /** Последняя поправка позиции: причина и тик. */
  lastTeleport: { reason: string; tick: number } | null = null;
  /** Все игроки онлайн (join/leave). */
  roster = new Map<number, RosterEntry>();
  /** Игроки в зоне видимости (spawn/despawn). */
  visible = new Map<number, Entity>();
  chunks = new Map<string, Chunk>();
  center: { x: number; z: number } | null = null;
  ticks: TickStat[] = [];
  flashes: Flash[] = [];
  /** Лента событий: отдельное кольцо на каждый тип. */
  log = new Map<EventType, LogEntry[]>();
  private seq = 0;
  chat: ChatLine[] = [];
  kicked: string | null = null;
  /** Номер последнего тика (−1 — тиков ещё не было). */
  tick = -1;
  /** Счётчики событий по типам за всё время — для ленты. */
  counts = new Map<EventType, number>();

  /** Применяет пачку одного тика (порядок — как прислал сервер). */
  apply(events: ServerEvent[]): Effects {
    const fx: Effects = { tpacks: [], repositioned: false };
    let loaded = 0;
    let unloaded = 0;
    const flashes: Flash[] = [];
    // Номер тика пачки известен только из последнего события tick.
    const last = events[events.length - 1];
    const batchTick = last?.t === "tick" ? last.n : this.tick + 1;

    for (const ev of events) {
      this.counts.set(ev.t, (this.counts.get(ev.t) ?? 0) + 1);
      if (ev.t !== "tick") {
        let ring = this.log.get(ev.t);
        if (!ring) this.log.set(ev.t, (ring = []));
        pushCapped(ring, { seq: this.seq++, tick: batchTick, t: ev.t, text: describe(ev) }, MAX_LOG);
      }
      switch (ev.t) {
        case "welcome":
          this.me = { id: ev.id, name: ev.name, x: ev.x, y: ev.y, z: ev.z, yaw: ev.yaw, pitch: ev.pitch };
          this.world = ev.world;
          this.viewDistance = ev.viewDistance;
          this.maxChat = ev.maxChat;
          this.placeable = ev.placeable;
          this.pendingTp = ev.tp;
          fx.tpacks.push(ev.tp);
          fx.repositioned = true;
          break;
        case "teleport":
          if (this.me) Object.assign(this.me, { x: ev.x, y: ev.y, z: ev.z, yaw: ev.yaw, pitch: ev.pitch });
          this.pendingTp = ev.tp;
          this.lastTeleport = { reason: ev.reason, tick: batchTick };
          if (ev.reason !== "resend") {
            pushCapped(this.chat, { kind: "teleport", text: `Поправка позиции: ${teleportReason(ev.reason)}` }, MAX_CHAT);
          }
          fx.tpacks.push(ev.tp);
          fx.repositioned = true;
          break;
        case "join":
          this.roster.set(ev.id, { id: ev.id, name: ev.name, kind: ev.kind });
          break;
        case "leave":
          this.roster.delete(ev.id);
          this.visible.delete(ev.id);
          break;
        case "spawn":
          if (ev.id === this.me?.id) break; // себя сервер не присылает, но на всякий случай
          this.visible.set(ev.id, { id: ev.id, name: ev.name, kind: ev.kind, x: ev.x, y: ev.y, z: ev.z, yaw: ev.yaw, pitch: ev.pitch });
          break;
        case "despawn":
          this.visible.delete(ev.id);
          break;
        case "move": {
          const e = this.visible.get(ev.id);
          if (e) Object.assign(e, { x: ev.x, y: ev.y, z: ev.z, yaw: ev.yaw, pitch: ev.pitch });
          break;
        }
        case "chat":
          pushCapped(this.chat, { kind: "chat", name: ev.name, text: ev.text, self: ev.id === this.me?.id }, MAX_CHAT);
          break;
        case "system":
          pushCapped(this.chat, { kind: "system", text: ev.text }, MAX_CHAT);
          break;
        case "center":
          this.center = { x: ev.x, z: ev.z };
          break;
        case "chunk":
          this.setChunk(ev);
          loaded++;
          flashes.push({ x: ev.x, z: ev.z, kind: "load", tick: batchTick });
          break;
        case "unchunk":
          this.chunks.delete(chunkKey(ev.x, ev.z));
          unloaded++;
          flashes.push({ x: ev.x, z: ev.z, kind: "unload", tick: batchTick });
          break;
        case "block":
          this.setBlockTop(ev.x, ev.z, ev.top, ev.topY);
          break;
        case "kick":
          this.kicked = ev.reason;
          fx.kicked = ev.reason;
          pushCapped(this.chat, { kind: "kick", text: `Сервер отключил: ${ev.reason}` }, MAX_CHAT);
          break;
        case "tick":
          this.tick = ev.n;
          this.pushTick(ev, loaded, unloaded);
          break;
      }
    }
    // Вспышки старше FLASH_TICKS забываем; в пачке без tick — не трогаем.
    this.flashes = this.flashes.filter((f) => batchTick - f.tick < FLASH_TICKS).concat(flashes);
    return fx;
  }

  /** tpack отправлен: сервер снова принимает перемещения. */
  acked(tp: number) {
    if (this.pendingTp === tp) this.pendingTp = null;
  }

  /** Можно ли сейчас двигаться (вошли и нет неподтверждённого телепорта). */
  canMove(): boolean {
    return this.me !== null && this.pendingTp === null && this.kicked === null;
  }

  private setChunk(ev: ChunkEvent) {
    const key = chunkKey(ev.x, ev.z);
    const prev = this.chunks.get(key);
    this.chunks.set(key, {
      x: ev.x,
      z: ev.z,
      palette: ev.palette.slice(),
      top: ev.top.slice(),
      height: ev.height.slice(),
      version: (prev?.version ?? 0) + 1,
    });
  }

  /** Новый верх колонки (x, z) после изменения блока. */
  private setBlockTop(x: number, z: number, top: string, topY: number) {
    const c = this.chunks.get(chunkKey(Math.floor(x / 16), Math.floor(z / 16)));
    if (!c) return; // чанк не загружен — сервер такое и не пришлёт
    let idx = c.palette.indexOf(top);
    if (idx < 0) {
      if (c.palette.length >= 256) return;
      idx = c.palette.push(top) - 1;
    }
    const i = (z - c.z * 16) * 16 + (x - c.x * 16);
    c.top[i] = idx;
    c.height[i] = topY;
    c.version++;
  }

  private pushTick(ev: TickEvent, loaded: number, unloaded: number) {
    pushCapped(this.ticks, { n: ev.n, mspt: ev.mspt, tps: ev.tps, msgs: ev.msgs, bytes: ev.bytes, loaded, unloaded }, HISTORY);
  }

  /** Последние limit событий выбранных типов, новые первыми. */
  recent(types: ReadonlySet<EventType>, limit: number): LogEntry[] {
    const out: LogEntry[] = [];
    for (const t of types) out.push(...(this.log.get(t) ?? []));
    return out.sort((a, b) => b.seq - a.seq).slice(0, limit);
  }

  /** Верхний блок колонки (x, z) по загруженным чанкам: имя и высота. */
  topAt(x: number, z: number): { block: string; y: number } | null {
    const bx = Math.floor(x);
    const bz = Math.floor(z);
    const c = this.chunks.get(chunkKey(Math.floor(bx / 16), Math.floor(bz / 16)));
    if (!c) return null;
    const i = (bz - c.z * 16) * 16 + (bx - c.x * 16);
    return { block: c.palette[c.top[i]], y: c.height[i] };
  }

  /** Чанк (cx, cz) в квадрате видимости вокруг center. */
  inView(cx: number, cz: number): boolean {
    if (!this.center) return false;
    const r = this.viewDistance;
    return Math.abs(cx - this.center.x) <= r && Math.abs(cz - this.center.z) <= r;
  }
}

/**
 * Ограничитель частоты перемещений: сервер принимает move не чаще раза в
 * тик (50 мс) и не больше 40 сообщений в секунду всего (с чатом и блоками).
 */
export class Throttle {
  private last = -Infinity;
  readonly intervalMs: number;
  // Без parameter properties: node --test снимает типы, но не переписывает код.
  constructor(intervalMs: number) {
    this.intervalMs = intervalMs;
  }
  /** true — можно отправить сейчас (и время отправки запоминается). */
  take(now: number): boolean {
    if (now - this.last < this.intervalMs) return false;
    this.last = now;
    return true;
  }
}

/**
 * Шаг перемещения игрока за dt секунд по направлению (dx, dz) со скоростью
 * speed блоков/с, с ограничением границей мира (чуть внутри, чтобы не
 * получить поправку out_of_bounds).
 */
export function step(
  me: { x: number; z: number },
  dx: number,
  dz: number,
  speed: number,
  dt: number,
  border: number,
): { x: number; z: number } {
  const len = Math.hypot(dx, dz);
  if (len === 0 || dt <= 0) return { x: me.x, z: me.z };
  const d = Math.min(speed * dt, speed * 0.25); // после паузы вкладки не прыгаем
  const lim = border - 0.5;
  const clamp = (v: number) => Math.max(-lim, Math.min(lim, v));
  return { x: clamp(me.x + (dx / len) * d), z: clamp(me.z + (dz / len) * d) };
}
