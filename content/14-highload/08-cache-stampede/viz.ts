// Живой визуализатор: шаги строятся из трассы запуска live/main.go —
// задачи runtime/trace (волна запросов и каждый запрос), регионы «БД»
// и «singleflight.Do», записи trace.Log о промахе и общем результате.
import { defineLive } from "../../../viz/src/index.ts";

export default defineLive({
  title: "Четыре запроса за одним ключом: без защиты и с singleflight",
  builder: "spans",
  file: "live/main.go",
  gomaxprocs: [1, 4],
});
