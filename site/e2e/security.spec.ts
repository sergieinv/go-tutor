// Этап 8: govulncheck в песочнице и задачи «найди и почини» с эксплойтами.
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

test("govulncheck находит уязвимый вызов и замолкает после обновления go.mod", async ({ page }) => {
  const lesson = lessonWith(/^govulncheck:/m);
  const gomod = fs.readFileSync(path.join(content, lesson, "example", "go.mod"), "utf8");
  const vuln = /require golang\.org\/x\/text (v[\d.]+)/.exec(gomod)?.[1];
  const sum = fs.readFileSync(path.join(content, lesson, "example", "go.sum"), "utf8");
  const fixed = [...sum.matchAll(/^golang\.org\/x\/text (v[\d.]+) h1:/gm)].map((m) => m[1]).find((v) => v !== vuln);
  expect(vuln && fixed, "в go.sum примера нужны уязвимая и исправленная версии").toBeTruthy();

  await page.goto(`/${lesson}/`);
  const playground = page.locator("[data-playground]");
  const result = playground.locator("[data-result]");
  await playground.getByRole("button", { name: "govulncheck" }).click();
  await expect(result.locator(".status")).toHaveText(/Код вызывает уязвимые функции/, { timeout: 60_000 });
  await expect(result).toContainText("GO-2022-1059");
  await expect(result).toContainText(/снимок vuln\.go\.dev от \d{4}-\d{2}-\d{2}/);

  // Обновляем версию в go.mod: модуль есть в кеше образа, сети не нужно.
  await playground.locator('[data-editor="go.mod"] .cm-content').click();
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.insertText(gomod.replace(`golang.org/x/text ${vuln}`, `golang.org/x/text ${fixed}`));
  await playground.getByRole("button", { name: "govulncheck" }).click();
  await expect(result.locator(".status")).toHaveText(/Уязвимых вызовов не найдено/, { timeout: 60_000 });
});

test("задача «найди и почини»: шаблон проходит функциональные тесты, эксплойты падают", async ({ page }) => {
  await page.goto(`/${lessonWith(/^taskKind: fix$/m)}/`);
  const task = page.locator("[data-task]");
  await expect(task.locator(".badge.fix")).toHaveText("найди и почини");
  await task.getByRole("button", { name: "Проверить" }).click();
  const tests = task.locator(".tests");
  await expect(tests).toBeVisible({ timeout: 60_000 });
  const exploits = tests.locator("li", { has: page.locator(".badge", { hasText: "эксплойт" }) });
  await expect(exploits.first()).toBeVisible();
  await expect(exploits.filter({ has: page.locator(".test-mark", { hasText: "✓" }) })).toHaveCount(0);
  await expect(tests.locator("li", { hasNotText: "эксплойт" }).locator(".test-mark", { hasText: "✗" })).toHaveCount(0);
});
