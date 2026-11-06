// Живой визуализатор: live/main.go запускается с -race,
// шаги строятся по отчёту детектора гонок.
import { defineLive } from "../../../viz/src/index.ts";

export default defineLive({
  title: "Гонки данных по отчёту -race",
  builder: "race",
  file: "live/main.go",
  gomaxprocs: [1, 4],
});
