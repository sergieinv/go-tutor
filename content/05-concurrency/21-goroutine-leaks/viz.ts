// Живой визуализатор: шаги строятся из трассы запуска live/main.go.
import { defineLive } from "../../../viz/src/index.ts";

export default defineLive({
  title: "Утечки горутин по реальному запуску",
  builder: "goroutines",
  file: "live/main.go",
  gomaxprocs: [1, 2, 4],
});
