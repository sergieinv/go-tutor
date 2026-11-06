import { defineConfig } from "@playwright/test";

// e2e идёт против полного стека: docker compose up --build → http://localhost:8080.
export default defineConfig({
  testDir: "e2e",
  timeout: 120_000,
  expect: { timeout: 30_000 },
  fullyParallel: false,
  reporter: [["list"]],
  use: {
    baseURL: process.env.BASE_URL ?? "http://localhost:8080",
    trace: "retain-on-failure",
  },
});
