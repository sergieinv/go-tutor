// Плеер шагов без DOM: индекс, листание в обе стороны, автопроигрывание.
// Интерфейс подписывается на onChange и рисует текущий шаг.
import type { Step } from "./types.ts";

export interface Clock {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

const realClock: Clock = {
  set: (fn, ms) => setTimeout(fn, ms),
  clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
};

export class Player {
  steps: Step[];
  index = 0;
  playing = false;
  /** Пауза между шагами в режиме «авто». */
  interval = 1800;
  private timer: unknown;
  private listeners: ((p: Player) => void)[] = [];
  private clock: Clock;

  constructor(steps: Step[], clock: Clock = realClock) {
    this.steps = steps;
    this.clock = clock;
  }

  get step(): Step {
    return this.steps[this.index];
  }
  get atStart() {
    return this.index === 0;
  }
  get atEnd() {
    return this.index >= this.steps.length - 1;
  }

  onChange(fn: (p: Player) => void) {
    this.listeners.push(fn);
  }

  private emit() {
    for (const fn of this.listeners) fn(this);
  }

  /** Новые шаги (живой режим после запуска) — с начала. */
  load(steps: Step[]) {
    this.pause();
    this.steps = steps;
    this.index = 0;
    this.emit();
  }

  go(i: number) {
    const next = Math.min(Math.max(i, 0), this.steps.length - 1);
    if (next === this.index) return;
    this.index = next;
    this.emit();
  }

  next(): boolean {
    if (this.atEnd) return false;
    this.go(this.index + 1);
    return true;
  }

  prev(): boolean {
    if (this.atStart) return false;
    this.go(this.index - 1);
    return true;
  }

  play() {
    if (this.playing) return;
    if (this.atEnd) this.index = 0;
    this.playing = true;
    this.emit();
    this.schedule();
  }

  pause() {
    if (!this.playing) return;
    this.playing = false;
    this.clock.clear(this.timer);
    this.emit();
  }

  toggle() {
    if (this.playing) this.pause();
    else this.play();
  }

  private schedule() {
    this.timer = this.clock.set(() => {
      if (!this.playing) return;
      this.next();
      if (this.atEnd) this.pause();
      else this.schedule();
    }, this.interval);
  }
}
