// Режим слайдов: content/<модуль>/slides.md — экраны через строку `---`,
// строка `@viz <урок>` вставляет визуализатор урока.
import fs from "node:fs";
import path from "node:path";
import { contentDir, lessonById, type Lesson, type Module } from "./content";
import { renderMarkdown } from "./markdown";

export interface Slide {
  html: string;
  viz?: Lesson;
}

const vizLine = /^@viz\s+(\S+)\s*$/m;

export function hasSlides(m: Module): boolean {
  return fs.existsSync(path.join(contentDir, m.slug, "slides.md"));
}

export async function loadSlides(m: Module): Promise<Slide[]> {
  const src = fs.readFileSync(path.join(contentDir, m.slug, "slides.md"), "utf8");
  return Promise.all(
    src.split(/^---$/m).map(async (part) => {
      const id = vizLine.exec(part)?.[1];
      const viz = id ? lessonById(id) : undefined;
      if (id && !viz?.hasViz) throw new Error(`${m.slug}/slides.md: у урока ${id} нет визуализатора`);
      return { html: await renderMarkdown(part.replace(vizLine, "")), viz };
    }),
  );
}
