// Разбор аннотированного примера в стиле gobyexample: блок комментариев `//`
// и код под ним до следующего блока образуют сегмент — две колонки на странице.

export interface Segment {
  doc: string[]; // абзацы пояснения; пустая строка `//` начинает новый абзац
  code: string;
}

export function segments(source: string): Segment[] {
  const out: { doc: string[][]; code: string[] }[] = [];
  let cur = { doc: [[]] as string[][], code: [] as string[] };
  for (const line of source.replace(/\n$/, "").split("\n")) {
    const trimmed = line.trim();
    // Директивы (//go:embed, //go:generate, //line) — код, а не пояснение.
    const directive = /^\/\/(go:|line )/.test(trimmed);
    if (trimmed.startsWith("//") && !directive) {
      if (cur.code.some((l) => l.trim() !== "")) {
        out.push(cur);
        cur = { doc: [[]], code: [] };
      }
      const text = trimmed.replace(/^\/\/\s?/, "");
      if (text === "") cur.doc.push([]);
      else cur.doc[cur.doc.length - 1].push(text);
    } else {
      cur.code.push(line);
    }
  }
  out.push(cur);

  return out
    .map((s) => ({
      doc: s.doc.filter((p) => p.length > 0).map((p) => p.join(" ")),
      code: trimBlankLines(s.code).join("\n"),
    }))
    .filter((s) => s.doc.length > 0 || s.code !== "");
}

function trimBlankLines(lines: string[]): string[] {
  let start = 0;
  let end = lines.length;
  while (start < end && lines[start].trim() === "") start++;
  while (end > start && lines[end - 1].trim() === "") end--;
  return lines.slice(start, end);
}
