import { defineConfig } from "astro/config";

export default defineConfig({
  trailingSlash: "always",
  build: { format: "directory" },
  vite: {
    // npm run dev: /api/* уходит в runner, запущенный рядом (go run ./runner).
    server: { proxy: { "/api": process.env.RUNNER_URL ?? "http://localhost:8081" } },
  },
});
