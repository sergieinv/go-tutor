// Живой визуализатор: шаги строятся из трассы запуска live/main.go —
// тики как задачи runtime/trace, фазы тика как регионы.
import { defineLive } from "../../../viz/src/index.ts";

export default defineLive({
  title: "Игровой цикл по реальному запуску: лёгкие тики, тяжёлый и догоняние",
  builder: "spans",
  file: "live/main.go",
});
