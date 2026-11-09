// Этап 14: поиск, переключатель темы, фолбэк песочницы без runner, узкий экран.
// Проходит и против полного стека (8080), и против статики без runner:
//   npm run build && npx astro preview --port 4331 --host 127.0.0.1
//   BASE_URL=http://127.0.0.1:4331 npx playwright test e2e/polish.spec.ts
import { expect, type Page, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const content = path.resolve(import.meta.dirname, "../../content");
const output = (lesson: string) => fs.readFileSync(path.join(content, lesson, "example/output.txt"), "utf8");

const searchInput = (page: Page) => page.getByRole("combobox", { name: "Поиск по курсу" });

test.describe("поиск", () => {
  test("русское слово в другой форме: «горутинами» находит урок о горутинах, Enter — переход", async ({ page }) => {
    await page.goto("/");
    await page.keyboard.press("/");
    const input = searchInput(page);
    await expect(input).toBeFocused();
    await input.fill("горутинами");
    const list = page.getByRole("listbox", { name: "Результаты поиска" });
    await expect(list.getByRole("option").first()).toBeVisible();
    await expect(list.getByRole("option").first().locator(".search-title")).toContainText(/[Гг]орутин/);
    await expect(list.locator("mark").first()).toBeVisible();

    // Стрелка вниз выбирает первый результат, Enter переходит к нему.
    await input.press("ArrowDown");
    await expect(input).toHaveAttribute("aria-activedescendant", "site-search-opt-0");
    const title = await list.getByRole("option").first().locator(".search-title").textContent();
    await input.press("Enter");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(title!);
  });

  test("идентификатор Go: errgroup и sync.Once находятся как есть", async ({ page }) => {
    await page.goto("/review/");
    const input = searchInput(page);
    await input.click();
    await input.fill("errgroup");
    const first = page.getByRole("option").first();
    await expect(first).toBeVisible();
    await expect(first.locator("mark").first()).toHaveText(/errgroup/i);
    await input.press("Enter");
    await expect(page).not.toHaveURL(/\/review\/$/);
    await expect(page.locator("main")).toContainText("errgroup");

    await searchInput(page).click();
    await searchInput(page).fill("sync.Once");
    await expect(page.getByRole("option").first().locator("mark").first()).toHaveText(/sync\.Once/i);
    // Esc закрывает список.
    await searchInput(page).press("Escape");
    await expect(page.getByRole("listbox", { name: "Результаты поиска" })).toBeHidden();
  });

  test("ничего не найдено — так и сказано", async ({ page }) => {
    await page.goto("/");
    await searchInput(page).fill("zzzzqqqq");
    await expect(page.getByRole("listbox", { name: "Результаты поиска" })).toContainText("Ничего не найдено");
  });
});

test.describe("тема", () => {
  const bg = (page: Page) => page.evaluate(() => getComputedStyle(document.body).backgroundColor);

  test("переключатель меняет data-theme, выбор переживает перезагрузку", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/");
    const html = page.locator("html");
    const toggle = page.getByRole("button", { name: /^Тема:/ });
    await expect(html).not.toHaveAttribute("data-theme", /.+/);
    await expect(toggle).toHaveAccessibleName(/системная/);

    await toggle.click(); // → светлая
    await expect(html).toHaveAttribute("data-theme", "light");
    await expect(toggle).toHaveAccessibleName(/светлая/);
    await toggle.click(); // → тёмная
    await expect(html).toHaveAttribute("data-theme", "dark");
    await expect(toggle).toHaveAccessibleName(/тёмная/);
    expect(await bg(page)).toBe("rgb(13, 17, 23)");

    await page.reload();
    await expect(html).toHaveAttribute("data-theme", "dark");
    expect(await bg(page)).toBe("rgb(13, 17, 23)");

    await toggle.click(); // → системная: атрибута нет, светлая по системе
    await expect(html).not.toHaveAttribute("data-theme", /.+/);
    expect(await bg(page)).toBe("rgb(255, 255, 255)");
    await page.reload();
    await expect(html).not.toHaveAttribute("data-theme", /.+/);
  });

  test("без вспышки: тема стоит до загрузки скриптов модулей", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/");
    await page.evaluate(() => localStorage.setItem("gotutor:theme", "dark"));
    // Скрипты модулей не грузятся вовсе — работает только встроенный скрипт в <head>.
    await page.route("**/_astro/*.js", (r) => r.abort());
    await page.goto("/02-basics/05-slices/");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await bg(page)).toBe("rgb(13, 17, 23)");
  });

  test("тёмная системная тема и явный выбор светлой", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "dark" });
    await page.goto("/");
    expect(await bg(page)).toBe("rgb(13, 17, 23)");
    await page.evaluate(() => localStorage.setItem("gotutor:theme", "light"));
    await page.goto("/slides/02-basics/");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(await bg(page)).toBe("rgb(255, 255, 255)");
  });
});

test.describe("песочница без runner", () => {
  const lesson = "02-basics/05-slices";

  test.beforeEach(async ({ page }) => {
    await page.route("**/api/**", (r) => r.abort());
    await page.goto(`/${lesson}/`);
  });

  test("неизменённый пример — показан проверенный output.txt", async ({ page }) => {
    const playground = page.locator("[data-playground]");
    await playground.getByRole("button", { name: "Запустить" }).click();
    const result = playground.locator("[data-result]");
    await expect(result).toContainText("Runner недоступен — показан проверенный вывод примера");
    await expect(result).toContainText("docker compose up");
    await expect(result.locator("pre")).toHaveText(output(lesson));
  });

  test("изменённый код — сообщение и кнопка с выводом исходного примера", async ({ page }) => {
    const playground = page.locator("[data-playground]");
    await playground.locator("[data-editor] .cm-content").click();
    await page.keyboard.press("ControlOrMeta+End");
    await page.keyboard.insertText("\n// правка\n");
    await playground.getByRole("button", { name: "Запустить" }).click();
    const result = playground.locator("[data-result]");
    await expect(result).toContainText("Runner недоступен");
    await expect(result).toContainText("Код изменён");
    await expect(result.locator("pre")).toHaveCount(0);
    await result.getByRole("button", { name: "Показать вывод исходного примера" }).click();
    await expect(result).toContainText("исходного примера, а не вашего кода");
    await expect(result.locator("pre")).toHaveText(output(lesson));
  });
});

test.describe("узкий экран 390 px", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  for (const url of ["/", "/02-basics/05-slices/", "/05-concurrency/07-select/", "/review/", "/game/", "/slides/02-basics/"]) {
    test(`нет горизонтальной прокрутки: ${url}`, async ({ page }) => {
      await page.goto(url);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(0);
    });
  }

  test("лупа раскрывает поле поиска", async ({ page }) => {
    await page.goto("/");
    const input = searchInput(page);
    await expect(input).toBeHidden();
    await page.getByRole("button", { name: "Поиск по курсу" }).click();
    await expect(input).toBeFocused();
    await input.fill("select");
    await expect(page.getByRole("option").first()).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });
});
