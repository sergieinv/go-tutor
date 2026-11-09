// Отрисовка браузерного клиента игрового сервера на canvas: карта мира
// сверху и мини-графики тиков. Читает GameState, ничего в нём не меняет.
import { chunkOf } from "./game-protocol.ts";
import { FLASH_TICKS, type Chunk, type GameState, type TickStat } from "./game-state.ts";

/** Цвета темы сайта (CSS-переменные) — читаются при смене темы. */
export interface Theme {
  bg: string;
  bgSoft: string;
  fg: string;
  muted: string;
  border: string;
  accent: string;
  ok: string;
  bad: string;
  dark: boolean;
}

export function readTheme(el: Element): Theme {
  const cs = getComputedStyle(el);
  const v = (name: string, fb: string) => cs.getPropertyValue(name).trim() || fb;
  return {
    bg: v("--bg", "#fff"),
    bgSoft: v("--bg-soft", "#f6f8fa"),
    fg: v("--fg", "#1f2328"),
    muted: v("--fg-muted", "#59636e"),
    border: v("--border", "#d1d9e0"),
    accent: v("--accent", "#00add8"),
    ok: v("--ok", "#1a7f37"),
    bad: v("--bad", "#cf222e"),
    // Итог темы сайта: и системная настройка, и переключатель в шапке.
    dark: cs.colorScheme === "dark",
  };
}

/** Вспышки чанков: загружен — жёлтый (зелёный терялся бы на траве), выгружен — красный темы. */
export const FLASH_LOAD = "#f5c518";

// Цвета блоков для карты сверху (RGB). Неизвестные — по хешу имени.
const BLOCK_COLORS: Record<string, [number, number, number]> = {
  "minecraft:grass_block": [93, 158, 63],
  "minecraft:stone": [136, 136, 136],
  "minecraft:cobblestone": [112, 112, 112],
  "minecraft:dirt": [122, 85, 53],
  "minecraft:bedrock": [50, 50, 50],
  "minecraft:oak_planks": [176, 138, 82],
  "minecraft:bricks": [156, 74, 58],
  "minecraft:stone_bricks": [122, 122, 122],
  "minecraft:glass": [200, 232, 240],
  "minecraft:white_wool": [234, 236, 236],
  "minecraft:red_wool": [176, 46, 38],
  "minecraft:blue_wool": [60, 68, 170],
  "minecraft:yellow_wool": [248, 198, 39],
  "minecraft:gold_block": [246, 208, 61],
  "minecraft:diamond_block": [98, 219, 214],
  "minecraft:air": [0, 0, 0],
};

export function blockColor(name: string): [number, number, number] {
  const c = BLOCK_COLORS[name];
  if (c) return c;
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return [80 + (h & 127), 80 + ((h >> 7) & 127), 80 + ((h >> 14) & 127)];
}

/** CSS-цвет блока (для списка блоков в интерфейсе). */
export const blockCSS = (name: string) => `rgb(${blockColor(name).join(" ")})`;

/** Цвет колонки: блок, высота над землёй — светлее, ниже — темнее. */
function shade(rgb: [number, number, number], dy: number): [number, number, number] {
  const k = Math.max(-0.5, Math.min(0.5, dy * 0.08));
  return rgb.map((c) => Math.round(k >= 0 ? c + (255 - c) * k : c * (1 + k))) as [number, number, number];
}

/** Камера карты: центр в блоках и масштаб (пикселей CSS на блок). */
export interface Camera {
  cx: number;
  cz: number;
  scale: number;
  width: number;
  height: number;
}

export function toScreen(cam: Camera, x: number, z: number): [number, number] {
  return [(x - cam.cx) * cam.scale + cam.width / 2, (z - cam.cz) * cam.scale + cam.height / 2];
}

export function toWorld(cam: Camera, px: number, py: number): [number, number] {
  return [(px - cam.width / 2) / cam.scale + cam.cx, (py - cam.height / 2) / cam.scale + cam.cz];
}

/**
 * Камера: центр — на игроке, в кадр помещается зона видимости (2R+1 чанков)
 * и ещё по чанку с краёв — видно, как чанки выгружаются.
 */
export function camera(state: GameState, width: number, height: number): Camera {
  const span = (2 * state.viewDistance + 3) * 16;
  const me = state.me;
  return { cx: me?.x ?? 0, cz: me?.z ?? 0, scale: Math.min(width, height) / span, width, height };
}

/** Кеш изображений чанков 16×16 (по версии чанка). */
export class ChunkImages {
  private cache = new Map<string, { version: number; canvas: HTMLCanvasElement }>();
  private groundY = -61;

  setGround(y: number) {
    if (y !== this.groundY) {
      this.groundY = y;
      this.cache.clear();
    }
  }

  get(c: Chunk): HTMLCanvasElement {
    const key = `${c.x},${c.z}`;
    const hit = this.cache.get(key);
    if (hit && hit.version === c.version) return hit.canvas;
    const canvas = hit?.canvas ?? document.createElement("canvas");
    canvas.width = 16;
    canvas.height = 16;
    const ctx = canvas.getContext("2d")!;
    const img = ctx.createImageData(16, 16);
    for (let i = 0; i < 256; i++) {
      const name = c.palette[c.top[i]] ?? "minecraft:air";
      const [r, g, b] = shade(blockColor(name), c.height[i] - this.groundY);
      img.data[i * 4] = r;
      img.data[i * 4 + 1] = g;
      img.data[i * 4 + 2] = b;
      img.data[i * 4 + 3] = name === "minecraft:air" ? 0 : 255;
    }
    ctx.putImageData(img, 0, 0);
    this.cache.set(key, { version: c.version, canvas });
    return canvas;
  }

  /** Забыть выгруженные чанки. */
  prune(state: GameState) {
    for (const k of this.cache.keys()) if (!state.chunks.has(k)) this.cache.delete(k);
  }
}

export interface MapOverlay {
  /** Цель клика (идём к ней). */
  target?: { x: number; z: number } | null;
  /** Клетка под курсором. */
  hover?: { x: number; z: number } | null;
}

/** Подготовить canvas к отрисовке в CSS-пикселях с учётом devicePixelRatio. */
export function fit(canvas: HTMLCanvasElement): { ctx: CanvasRenderingContext2D; w: number; h: number } {
  const dpr = Math.min(3, window.devicePixelRatio || 1);
  const w = Math.max(1, Math.round(canvas.clientWidth));
  const h = Math.max(1, Math.round(canvas.clientHeight));
  if (canvas.width !== Math.round(w * dpr) || canvas.height !== Math.round(h * dpr)) {
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
  }
  const ctx = canvas.getContext("2d")!;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  return { ctx, w, h };
}

/** Карта сверху: чанки, сетка, зона видимости, вспышки, граница, игроки. */
export function drawMap(
  canvas: HTMLCanvasElement,
  state: GameState,
  images: ChunkImages,
  theme: Theme,
  overlay: MapOverlay = {},
): Camera {
  const { ctx, w, h } = fit(canvas);
  const cam = camera(state, w, h);
  const s = cam.scale;
  ctx.fillStyle = theme.bgSoft;
  ctx.fillRect(0, 0, w, h);

  const [wx0, wz0] = toWorld(cam, 0, 0);
  const [wx1, wz1] = toWorld(cam, w, h);
  const c0x = chunkOf(wx0);
  const c0z = chunkOf(wz0);
  const c1x = chunkOf(wx1);
  const c1z = chunkOf(wz1);

  // 1. Чанки.
  if (state.world) images.setGround(state.world.groundY);
  ctx.imageSmoothingEnabled = false;
  for (const c of state.chunks.values()) {
    if (c.x < c0x || c.x > c1x || c.z < c0z || c.z > c1z) continue;
    const [px, py] = toScreen(cam, c.x * 16, c.z * 16);
    ctx.drawImage(images.get(c), px, py, 16 * s, 16 * s);
  }

  // 2. Сетка чанков.
  ctx.lineWidth = 1;
  ctx.strokeStyle = theme.dark ? "rgba(255,255,255,0.12)" : "rgba(0,0,0,0.12)";
  ctx.beginPath();
  for (let cx = c0x; cx <= c1x + 1; cx++) {
    const [px] = toScreen(cam, cx * 16, 0);
    ctx.moveTo(Math.round(px) + 0.5, 0);
    ctx.lineTo(Math.round(px) + 0.5, h);
  }
  for (let cz = c0z; cz <= c1z + 1; cz++) {
    const [, py] = toScreen(cam, 0, cz * 16);
    ctx.moveTo(0, Math.round(py) + 0.5);
    ctx.lineTo(w, Math.round(py) + 0.5);
  }
  ctx.stroke();

  // 3. Вспышки чанков, загруженных (жёлтые) и выгруженных (красные) за последние тики.
  for (const f of state.flashes) {
    const age = Math.max(0, state.tick - f.tick);
    const a = 0.55 * (1 - age / FLASH_TICKS);
    if (a <= 0) continue;
    const [px, py] = toScreen(cam, f.x * 16, f.z * 16);
    ctx.globalAlpha = a;
    ctx.fillStyle = f.kind === "load" ? FLASH_LOAD : theme.bad;
    ctx.fillRect(px, py, 16 * s, 16 * s);
  }
  ctx.globalAlpha = 1;

  // 4. Зона видимости и центральный чанк.
  if (state.center) {
    const r = state.viewDistance;
    const [zx, zy] = toScreen(cam, (state.center.x - r) * 16, (state.center.z - r) * 16);
    const side = (2 * r + 1) * 16 * s;
    ctx.strokeStyle = theme.accent;
    ctx.lineWidth = 2;
    ctx.setLineDash([6, 4]);
    ctx.strokeRect(zx, zy, side, side);
    ctx.setLineDash([]);
    const [cx, cy] = toScreen(cam, state.center.x * 16, state.center.z * 16);
    ctx.fillStyle = theme.accent;
    ctx.globalAlpha = 0.18;
    ctx.fillRect(cx, cy, 16 * s, 16 * s);
    ctx.globalAlpha = 1;
    ctx.strokeRect(cx + 1, cy + 1, 16 * s - 2, 16 * s - 2);
  }

  // 5. Граница мира: снаружи — штриховка.
  if (state.world) {
    const b = state.world.borderRadius;
    const [bx0, by0] = toScreen(cam, -b, -b);
    const [bx1, by1] = toScreen(cam, b, b);
    ctx.save();
    ctx.beginPath();
    ctx.rect(0, 0, w, h);
    ctx.rect(bx0, by0, bx1 - bx0, by1 - by0);
    ctx.clip("evenodd");
    ctx.fillStyle = theme.dark ? "rgba(0,0,0,0.55)" : "rgba(0,0,0,0.25)";
    ctx.fillRect(0, 0, w, h);
    ctx.strokeStyle = theme.bad;
    ctx.globalAlpha = 0.35;
    ctx.beginPath();
    for (let d = -h; d < w; d += 10) {
      ctx.moveTo(d, h);
      ctx.lineTo(d + h, 0);
    }
    ctx.stroke();
    ctx.restore();
    ctx.strokeStyle = theme.bad;
    ctx.lineWidth = 2;
    ctx.strokeRect(bx0, by0, bx1 - bx0, by1 - by0);
  }

  // 6. Клетка под курсором и цель клика.
  if (overlay.hover) {
    const [hx, hy] = toScreen(cam, Math.floor(overlay.hover.x), Math.floor(overlay.hover.z));
    ctx.strokeStyle = theme.fg;
    ctx.lineWidth = 1;
    ctx.strokeRect(hx + 0.5, hy + 0.5, Math.max(2, s - 1), Math.max(2, s - 1));
  }
  if (overlay.target) {
    const [tx, ty] = toScreen(cam, overlay.target.x, overlay.target.z);
    ctx.strokeStyle = theme.accent;
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(tx - 6, ty - 6);
    ctx.lineTo(tx + 6, ty + 6);
    ctx.moveTo(tx + 6, ty - 6);
    ctx.lineTo(tx - 6, ty + 6);
    ctx.stroke();
  }

  // 7. Игроки: видимые (spawn) и свой — крупнее, цветом акцента, со стрелкой направления.
  ctx.font = `600 12px ${getComputedStyle(canvas).fontFamily || "system-ui"}`;
  ctx.textAlign = "center";
  ctx.textBaseline = "bottom";
  const label = (text: string, x: number, y: number, color: string) => {
    ctx.lineWidth = 3;
    ctx.strokeStyle = theme.dark ? "rgba(13,17,23,0.85)" : "rgba(255,255,255,0.9)";
    ctx.strokeText(text, x, y);
    ctx.fillStyle = color;
    ctx.fillText(text, x, y);
  };
  for (const e of state.visible.values()) {
    const [px, py] = toScreen(cam, e.x, e.z);
    ctx.fillStyle = e.kind === "minecraft" ? "#e5534b" : "#d29922";
    ctx.strokeStyle = theme.dark ? "#0d1117" : "#fff";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.arc(px, py, 5, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();
    label(e.name, px, py - 8, theme.fg);
  }
  if (state.me) {
    const [px, py] = toScreen(cam, state.me.x, state.me.z);
    const yaw = (state.me.yaw * Math.PI) / 180;
    // yaw 0 = +Z (вниз на карте), 90 = −X (влево).
    const dx = -Math.sin(yaw);
    const dz = Math.cos(yaw);
    ctx.fillStyle = theme.accent;
    ctx.strokeStyle = theme.dark ? "#0d1117" : "#fff";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(px + dx * 13, py + dz * 13);
    ctx.lineTo(px - dz * 6, py + dx * 6);
    ctx.lineTo(px + dz * 6, py - dx * 6);
    ctx.closePath();
    ctx.fill();
    ctx.beginPath();
    ctx.arc(px, py, 7, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();
    label(`${state.me.name} (вы)`, px, py - 11, theme.accent);
  }
  return cam;
}

export type Series = "msgs" | "bytes" | "mspt" | "tps";

/**
 * Мини-график последних тиков: столбики выбранной величины, сверху —
 * отметки загруженных (жёлтые) и выгруженных (красные) в тике чанков.
 */
export function drawChart(canvas: HTMLCanvasElement, ticks: TickStat[], series: Series, theme: Theme, history: number) {
  const { ctx, w, h } = fit(canvas);
  ctx.fillStyle = theme.bgSoft;
  ctx.fillRect(0, 0, w, h);
  const top = 14;
  const plotH = h - top - 2;
  const values = ticks.map((t) => t[series]);
  let max = Math.max(1e-9, ...values);
  if (series === "tps") max = Math.max(20, max);
  const bw = w / history;
  const x0 = w - ticks.length * bw;
  ctx.fillStyle = series === "mspt" ? theme.bad : theme.accent;
  for (let i = 0; i < ticks.length; i++) {
    const v = values[i];
    const bh = Math.max(v > 0 ? 1 : 0, (v / max) * plotH);
    ctx.fillRect(x0 + i * bw, h - 1 - bh, Math.max(1, bw - 1), bh);
  }
  for (let i = 0; i < ticks.length; i++) {
    const t = ticks[i];
    if (t.loaded) {
      ctx.fillStyle = FLASH_LOAD;
      ctx.fillRect(x0 + i * bw, 2, Math.max(1, bw - 1), 4);
    }
    if (t.unloaded) {
      ctx.fillStyle = theme.bad;
      ctx.fillRect(x0 + i * bw, 7, Math.max(1, bw - 1), 4);
    }
  }
  // Подпись максимума шкалы.
  ctx.fillStyle = theme.muted;
  ctx.font = `11px ${getComputedStyle(canvas).fontFamily || "system-ui"}`;
  ctx.textAlign = "right";
  ctx.textBaseline = "top";
  const fmt = series === "mspt" || series === "tps" ? max.toFixed(series === "mspt" ? 3 : 1) : String(Math.round(max));
  ctx.fillText(`макс. ${fmt}`, w - 4, 1);
}
