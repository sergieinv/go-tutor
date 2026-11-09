// Живой визуализатор: шаги строятся из трассы запуска live/main.go.
import { defineLive } from "../../../viz/src/index.ts";

export default defineLive({
  title: "Конкурентные Push по реальному запуску",
  builder: "spans",
  file: "live/main.go",
  gomaxprocs: [1, 4],
});
