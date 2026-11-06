// Формат визуализаций. Сцена — плоский список фигур со стабильными id:
// рендер сопоставляет фигуры соседних шагов по id и анимирует разницу,
// поэтому шаги листаются в обе стороны без отдельной логики «назад».

export interface Point {
  x: number;
  y: number;
}

interface Base {
  /** Стабильный id: одна и та же сущность на разных шагах — один id. */
  id: string;
  /** Классы оформления из viz.css: v-cell, v-hl, v-muted… */
  cls?: string;
}

export interface RectShape extends Base {
  type: "rect";
  x: number;
  y: number;
  w: number;
  h: number;
  rx?: number;
}

export interface TextShape extends Base {
  type: "text";
  x: number;
  y: number;
  text: string;
  anchor?: "start" | "middle" | "end";
}

export interface CircleShape extends Base {
  type: "circle";
  x: number;
  y: number;
  r: number;
}

export interface PathShape extends Base {
  type: "path";
  d: string;
  /** Наконечник стрелки на конце пути. */
  arrow?: boolean;
}

export type Shape = RectShape | TextShape | CircleShape | PathShape;
export type Scene = Shape[];

export interface Step {
  /** Строки кода (с 1), которые подсвечиваются на шаге. */
  lines?: number[];
  /** Текстовое описание шага; `код` в обратных кавычках. Обязательно — это и подпись, и доступность. */
  note: string;
  scene: Scene;
  /** Метка времени шага в живом режиме: «12,4 мс». */
  time?: string;
}

/** Сценарный визуализатор: шаги описаны заранее. */
export interface Scenario {
  kind: "scenario";
  title: string;
  /** Код, по которому идут шаги. */
  code: string;
  width: number;
  height: number;
  steps: Step[];
}

/** Живой визуализатор: шаги строятся из запуска кода ученика в runner. */
export interface LiveSpec {
  kind: "live";
  title: string;
  /**
   * Построитель шагов (viz/src/live/index.ts):
   * select — операторы select по трассе; goroutines — стадии и передачи между
   * горутинами, утечки (pipeline, fan-in/out); race — отчёт детектора гонок.
   */
  builder: "select" | "goroutines" | "race";
  /** Программа относительно папки урока; её проверяет lessonlint. */
  file: string;
  /** Варианты GOMAXPROCS в интерфейсе; пусто — без выбора. */
  gomaxprocs?: number[];
}

export type Viz = Scenario | LiveSpec;
