// Индекс поиска: собирается при сборке в dist/search-index.json.gz. Сжат gzip
// (nginx учебника не сжимает ответы), браузер распаковывает его DecompressionStream.
import type { APIRoute } from "astro";
import { gzipSync } from "node:zlib";
import { buildSearchIndex } from "../lib/search-index";

export const GET: APIRoute = () =>
  new Response(gzipSync(JSON.stringify(buildSearchIndex()), { level: 9 }), {
    headers: { "Content-Type": "application/gzip" },
  });
