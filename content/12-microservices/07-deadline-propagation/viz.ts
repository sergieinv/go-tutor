// Живой визуализатор: шаги строятся из трассы запуска live/main.go —
// задачи runtime/trace (сервисы цепочки), регионы (работа) и записи
// trace.Log с остатком бюджета.
import { defineLive } from "../../../viz/src/index.ts";

export default defineLive({
  title: "Бюджет времени через три сервиса по реальному запуску",
  builder: "spans",
  file: "live/main.go",
});
