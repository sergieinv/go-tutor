// Рендер сцены в SVG с сопоставлением фигур по id. Позиция задаётся через
// CSS transform — её переход анимирует viz.css; при prefers-reduced-motion
// переходы выключены и смена шага мгновенная.
import { ARROW_HEAD } from "./primitives.ts";
import type { Scene, Shape } from "./types.ts";

const NS = "http://www.w3.org/2000/svg";
let uid = 0;

export interface Surface {
  svg: SVGSVGElement;
  layer: SVGGElement;
  marker: string;
  byId: Map<string, SVGElement>;
}

/** Готовит SVG: слой фигур и маркер стрелки с уникальным id (плееров на странице несколько). */
export function createSurface(svg: SVGSVGElement, width: number, height: number): Surface {
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  svg.replaceChildren();
  const marker = `v-arrow-${++uid}`;
  const defs = document.createElementNS(NS, "defs");
  const m = document.createElementNS(NS, "marker");
  // Размер в единицах схемы, а не в толщинах линии: наконечник одинаков у всех
  // стрелок. Основание — в конце пути, остриё — на ARROW_HEAD дальше, ровно в цели.
  const size = String(ARROW_HEAD);
  for (const [k, v] of Object.entries({ id: marker, viewBox: `0 0 ${size} ${size}`, refX: "0", refY: String(ARROW_HEAD / 2), markerWidth: size, markerHeight: size, markerUnits: "userSpaceOnUse", orient: "auto" }))
    m.setAttribute(k, v);
  const tip = document.createElementNS(NS, "path");
  tip.setAttribute("d", `M0,0 L${size},${ARROW_HEAD / 2} L0,${size} z`);
  tip.setAttribute("class", "v-arrowhead");
  m.append(tip);
  defs.append(m);
  const layer = document.createElementNS(NS, "g");
  svg.append(defs, layer);
  return { svg, layer, marker, byId: new Map() };
}

const tag = (s: Shape) => (s.type === "rect" ? "rect" : s.type === "text" ? "text" : s.type === "circle" ? "circle" : "path");

function update(el: SVGElement, s: Shape, marker: string) {
  el.setAttribute("class", `v-shape ${s.cls ?? ""}`.trim());
  switch (s.type) {
    case "rect":
      el.style.transform = `translate(${s.x}px, ${s.y}px)`;
      el.setAttribute("width", String(s.w));
      el.setAttribute("height", String(s.h));
      el.setAttribute("rx", String(s.rx ?? 3));
      break;
    case "text":
      el.style.transform = `translate(${s.x}px, ${s.y}px)`;
      el.setAttribute("text-anchor", s.anchor ?? "start");
      if (el.textContent !== s.text) el.textContent = s.text;
      break;
    case "circle":
      el.style.transform = `translate(${s.x}px, ${s.y}px)`;
      el.setAttribute("r", String(s.r));
      break;
    case "path":
      el.setAttribute("d", s.d);
      if (s.arrow) el.setAttribute("marker-end", `url(#${marker})`);
      else el.removeAttribute("marker-end");
      break;
  }
}

/** Приводит слой к сцене: новые фигуры появляются, исчезнувшие удаляются, общие плавно меняются. */
export function renderScene(surface: Surface, scene: Scene) {
  const seen = new Set<string>();
  let prev: SVGElement | null = null;
  for (const s of scene) {
    if (seen.has(s.id)) throw new Error(`повтор id фигуры ${s.id}`);
    seen.add(s.id);
    let el = surface.byId.get(s.id);
    if (el && el.tagName !== tag(s)) {
      el.remove();
      el = undefined;
    }
    if (!el) {
      el = document.createElementNS(NS, tag(s)) as SVGElement;
      el.dataset.id = s.id;
      update(el, s, surface.marker);
      el.classList.add("v-enter");
      surface.byId.set(s.id, el);
    } else {
      update(el, s, surface.marker);
    }
    // Порядок в DOM = порядок отрисовки; двигаем узел, только если он не на месте,
    // иначе браузер сбросит идущий переход.
    const want: ChildNode | null = prev ? prev.nextSibling : surface.layer.firstChild;
    if (want !== el) surface.layer.insertBefore(el, want);
    prev = el;
  }
  for (const [id, el] of surface.byId) {
    if (!seen.has(id)) {
      el.remove();
      surface.byId.delete(id);
    }
  }
}
