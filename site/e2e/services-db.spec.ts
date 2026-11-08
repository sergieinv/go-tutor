// Этап 9: уроки на настоящих базах профиля labs, задачи «приведи к виду
// для ревью», второй (живой) визуализатор урока и таблицы в тексте.
// Нужен docker compose --profile labs up.
import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const content = path.resolve(import.meta.dirname, "../../content");

/** Первый урок, у которого во frontmatter lesson.md есть строка, подходящая под re. */
function lessonWith(re: RegExp): string {
  for (const mod of fs.readdirSync(content).sort()) {
    const modDir = path.join(content, mod);
    if (!fs.statSync(modDir).isDirectory()) continue;
    for (const lesson of fs.readdirSync(modDir).sort()) {
      const md = path.join(modDir, lesson, "lesson.md");
      if (!fs.existsSync(md)) continue;
      const front = /^---\n([\s\S]*?)\n---\n/.exec(fs.readFileSync(md, "utf8"))?.[1] ?? "";
      if (re.test(front)) return `${mod}/${lesson}`;
    }
  }
  throw new Error(`нет урока с ${re}`);
}

test("урок с labs: пример работает на настоящем PostgreSQL и совпадает с output.txt", async ({ page }) => {
  const lesson = lessonWith(/labs: \[postgres\]/);
  const want = fs.readFileSync(path.join(content, lesson, "example", "output.txt"), "utf8").trim().split("\n");
  await page.goto(`/${lesson}/`);
  const playground = page.locator("[data-playground]");
  await expect(playground.locator(".labs-note")).toContainText("docker compose --profile labs up");
  await playground.getByRole("button", { name: "Запустить" }).click();
  const result = playground.locator("[data-result]");
  // Первая и последняя строки вывода: пример дошёл до конца на настоящей базе,
  // а не напечатал подсказку про профиль labs.
  await expect(result).toContainText(want[0], { timeout: 60_000 });
  await expect(result).toContainText(want[want.length - 1]);
  await expect(result).not.toContainText("нужен PostgreSQL");
});

test("задача «приведи к виду для ревью»: значок, ссылка на чек-лист, падают только проверки ревью", async ({ page }) => {
  await page.goto(`/${lessonWith(/^taskKind: review$/m)}/`);
  const task = page.locator("[data-task]");
  await expect(task.locator(".badge.fix")).toHaveText("приведи к виду для ревью");
  await expect(task.getByRole("link", { name: "чек-листа ревью Go-кода" })).toHaveAttribute("href", "/review/");
  await task.getByRole("button", { name: "Проверить" }).click();
  const tests = task.locator(".tests");
  await expect(tests).toBeVisible({ timeout: 60_000 });
  const review = tests.locator("li", { has: page.locator(".badge", { hasText: "ревью" }) });
  await expect(review.first()).toBeVisible();
  await expect(review.filter({ has: page.locator(".test-mark", { hasText: "✓" }) })).toHaveCount(0);
  await expect(tests.locator("li", { hasNotText: "ревью" }).locator(".test-mark", { hasText: "✗" })).toHaveCount(0);

  await page.goto("/review/");
  await expect(page.locator("h1")).toHaveText("Чек-лист ревью Go-кода");
});

test("урок с двумя визуализаторами: сценарный и живой, живой показывает зависших", async ({ page }) => {
  const lesson = fs
    .readdirSync(content)
    .flatMap((m) => (fs.statSync(path.join(content, m)).isDirectory() ? fs.readdirSync(path.join(content, m)).map((l) => `${m}/${l}`) : []))
    .find((id) => fs.existsSync(path.join(content, id, "viz-live.ts")));
  expect(lesson, "нужен урок с viz-live.ts").toBeTruthy();
  await page.goto(`/${lesson}/`);
  const vizs = page.locator("[data-viz]");
  await expect(vizs).toHaveCount(2);
  await expect(vizs.nth(0)).toHaveAttribute("data-kind", "scenario");
  const live = vizs.nth(1);
  await expect(live).toHaveAttribute("data-kind", "live");
  await live.getByRole("button", { name: "Запустить и построить" }).click();
  await expect(live.locator("[data-counter]")).toHaveText(/^Шаг 1 из \d+$/, { timeout: 30_000 });
  await live.locator("[data-act=seek]").press("End");
  await expect(live.locator("[data-note]")).toContainText("Не завершились к выходу из main");
});

test("таблицы из markdown рендерятся таблицами", async ({ page }) => {
  const lesson = lessonWith(/labs: \[mysql\]/);
  await page.goto(`/${lesson}/`);
  await expect(page.locator(".prose table").first()).toBeVisible();
  await expect(page.locator(".prose").first()).not.toContainText("| ---");
});
