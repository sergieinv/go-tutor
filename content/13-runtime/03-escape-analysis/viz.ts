// Живой визуализатор: runner собирает live/main.go с -gcflags=-m,
// и каждое сообщение escape analysis о main.go становится шагом.
import { defineLive } from "../../../viz/src/index.ts";

export default defineLive({
  title: "Стек или куча: решение компилятора",
  builder: "escape",
  file: "live/main.go",
});
