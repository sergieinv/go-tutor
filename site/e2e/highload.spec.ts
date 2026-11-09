// Этап 12: модуль «Высокие нагрузки» — урок на Valkey профиля labs, живые
// визуализаторы на построителе spans и сценарий шардирования.
// Нужен docker compose --profile labs up.
import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const content = path.resolve(import.meta.dirname, "../../content");

test("урок с labs: [valkey]: пример работает на настоящем Valkey и совпадает с output.txt", async ({ page }) => {
  const lesson = "14-highload/07-valkey-cache";
  const want = fs.readFileSync(path.join(content, lesson, "example", "output.txt"), "utf8").trim().split("\n");
  await page.goto(`/${lesson}/`);
  const playground = page.locator("[data-playground]");
  await expect(playground.locator(".labs-note")).toContainText("Valkey");
  await expect(playground.locator(".labs-note")).toContainText("VALKEY_PREFIX");
  await playground.getByRole("button", { name: "Запустить" }).click();
  const result = playground.locator("[data-result]");
  await expect(result).toContainText(want[0], { timeout: 90_000 });
  await expect(result).toContainText(want[want.length - 1]);
});

for (const lesson of ["14-highload/08-cache-stampede", "14-highload/20-lock-free-stack"]) {
  test(`живой визуализатор spans: ${lesson}`, async ({ page }) => {
    await page.goto(`/${lesson}/`);
    const live = page.locator('[data-viz][data-kind="live"]');
    await live.getByRole("button", { name: "Запустить и построить" }).click();
    await expect(live.locator("[data-counter]")).toHaveText(/^Шаг 1 из \d+$/, { timeout: 90_000 });
    await live.locator("[data-act=seek]").press("End");
    await expect(live.locator("[data-note]")).not.toBeEmpty();
    await expect(live.locator("svg text").first()).toBeVisible();
  });
}

test("сценарий шардирования: шаги переключаются, схема рисуется", async ({ page }) => {
  await page.goto("/14-highload/12-sharding/");
  const viz = page.locator('[data-viz][data-kind="scenario"]');
  await expect(viz.locator("[data-counter]")).toHaveText(/^Шаг 1 из \d+$/);
  await viz.locator("[data-act=next]").click();
  await expect(viz.locator("[data-counter]")).toHaveText(/^Шаг 2 из \d+$/);
  await expect(viz.locator("svg text").first()).toBeVisible();
});
