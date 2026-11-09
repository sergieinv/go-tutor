// Этап 11: модуль «Механизмы рантайма» — диагностика компилятора под выводом
// примера, живой визуализатор escape analysis и кнопка pprof.
import { expect, test } from "@playwright/test";

test("пример с -gcflags=-m: под выводом программы — диагностика компилятора", async ({ page }) => {
  await page.goto("/13-runtime/03-escape-analysis/");
  const playground = page.locator("[data-playground]");
  await expect(playground).toContainText('-gcflags="-m"');
  await playground.getByRole("button", { name: "Запустить" }).click();
  const result = playground.locator("[data-result]");
  await expect(result.locator(".stream-label", { hasText: 'go build -gcflags="-m"' })).toBeVisible({ timeout: 90_000 });
  await expect(result).toContainText(/escapes to heap|moved to heap/);
});

test("живой визуализатор escape: шаги «стек/куча» по выводу компилятора", async ({ page }) => {
  await page.goto("/13-runtime/03-escape-analysis/");
  const live = page.locator('[data-viz][data-kind="live"]');
  await live.getByRole("button", { name: "Запустить и построить" }).click();
  await expect(live.locator("[data-counter]")).toHaveText(/^Шаг 1 из \d+$/, { timeout: 90_000 });
  await expect(live.locator("[data-note]")).toContainText("-gcflags=-m");
  await live.locator("[data-act=next]").click();
  await expect(live.locator("[data-note]")).toContainText(/Строка \d+/);
  await expect(live.locator("svg text", { hasText: "Куча" }).first()).toBeVisible();
});

test("пример с runtime/pprof: кнопка pprof показывает go tool pprof -top", async ({ page }) => {
  await page.goto("/13-runtime/12-pprof-trace/");
  const playground = page.locator("[data-playground]");
  await playground.getByRole("button", { name: "pprof" }).click();
  const result = playground.locator("[data-result]");
  await expect(result.locator(".stream-label", { hasText: /go tool pprof -top \w+\.pprof/ }).first()).toBeVisible({ timeout: 90_000 });
  await expect(result).toContainText("flat%");
});
