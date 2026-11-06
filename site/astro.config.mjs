import { defineConfig } from "astro/config";
import { fileURLToPath } from "node:url";

// Корень репозитория: сайт импортирует движок из viz/ и viz.ts уроков из content/.
const repoRoot = fileURLToPath(new URL("..", import.meta.url));
const runnerURL = process.env.RUNNER_URL ?? "http://localhost:8081";

// npm run dev: /api/* уходит в runner, запущенный рядом (go run ./runner).
// vite.server.proxy в dev-сервере Astro 7 не срабатывает: его middleware
// (trailingSlash и др.) встают в начало цепочки и отвечают 404 раньше.
// Пересылку ставим в начало тем же способом, но позже — в post-хуке.
const runnerProxy = {
  name: "gotutor-runner-proxy",
  configureServer(server) {
    return () => server.middlewares.stack.unshift({ route: "/api", handle: async (req, res) => {
      const chunks = [];
      for await (const c of req) chunks.push(c);
      try {
        const r = await fetch(runnerURL + "/api" + req.url, {
          method: req.method,
          headers: { "Content-Type": req.headers["content-type"] ?? "application/json" },
          body: req.method === "GET" || req.method === "HEAD" ? undefined : Buffer.concat(chunks),
        });
        res.statusCode = r.status;
        res.setHeader("Content-Type", r.headers.get("content-type") ?? "application/json");
        res.end(Buffer.from(await r.arrayBuffer()));
      } catch {
        res.statusCode = 502;
        res.end("runner недоступен");
      }
    } });
  },
};

export default defineConfig({
  trailingSlash: "always",
  build: { format: "directory" },
  vite: {
    plugins: [runnerProxy],
    server: { fs: { allow: [repoRoot] } },
  },
});
