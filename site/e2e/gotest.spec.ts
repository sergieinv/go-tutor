// Этап 7: примеры с *_test.go — тесты, покрытие, бенчмарки и fuzzing в песочнице.
import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const content = path.resolve(import.meta.dirname, "../../content");

/** Первый урок, у которого в example/*_test.go есть строка, подходящая под re. */
function lessonWith(re: RegExp): string {
  for (const mod of fs.readdirSync(content).sort()) {
    const modDir = path.join(content, mod);
    if (!fs.statSync(modDir).isDirectory()) continue;
    for (const lesson of fs.readdirSync(modDir).sort()) {
      const ex = path.join(modDir, lesson, "example");
      if (!fs.existsSync(ex)) continue;
      const tests = fs.readdirSync(ex).filter((f) => f.endsWith("_test.go"));
      if (tests.some((f) => re.test(fs.readFileSync(path.join(ex, f), "utf8")))) return `${mod}/${lesson}`;
    }
  }
  throw new Error(`нет урока с ${re}`);
}

test("пример с тестами: тесты, покрытие и правка тестов", async ({ page }) => {
  await page.goto(`/${lessonWith(/^func Test/m)}/`);
  const playground = page.locator("[data-playground]");
  const result = playground.locator("[data-result]");
  await expect(playground.locator("figcaption", { hasText: "main_test.go" })).toBeVisible();

  await playground.getByRole("button", { name: "Запустить тесты" }).click();
  await expect(result).toContainText("Тесты пройдены");
  await expect(result).toContainText("--- PASS");

  await playground.getByRole("button", { name: "Покрытие" }).click();
  await expect(result).toContainText("go tool cover -func");
  await expect(result).toContainText("total:");

  // Сломанный тест: песочница показывает падение, а не «Готово».
  const testEditor = playground.locator('[data-editor="main_test.go"]');
  // Дописываем в конец: CodeMirror рисует только видимые строки, текст целиком из DOM не прочитать.
  await testEditor.locator(".cm-content").click();
  await page.keyboard.press("ControlOrMeta+End");
  await page.keyboard.insertText('\n\nfunc TestE2EFails(t *testing.T) { t.Fatal("e2e") }\n');
  await playground.getByRole("button", { name: "Запустить тесты" }).click();
  await expect(result).toContainText("Тесты не прошли");
  await expect(result).toContainText("--- FAIL: TestE2EFails");
});

test("пример с бенчмарком запускает бенчмарки", async ({ page }) => {
  await page.goto(`/${lessonWith(/^func Benchmark\w*\(b \*testing\.B\)/m)}/`);
  const playground = page.locator("[data-playground]");
  await playground.getByRole("button", { name: "Бенчмарки" }).click();
  await expect(playground.locator("[data-result]")).toContainText("Бенчмарки выполнены");
  await expect(playground.locator("[data-result]")).toContainText("ns/op");
});

test("пример с fuzz-тестом запускает фаззер", async ({ page }) => {
  await page.goto(`/${lessonWith(/^func Fuzz\w*\(f \*testing\.F\)/m)}/`);
  const playground = page.locator("[data-playground]");
  await playground.getByRole("button", { name: /^Fuzzing/ }).first().click();
  await expect(playground.locator("[data-result] .status")).toHaveText(/Фаззер нашёл вход|Fuzzing не нашёл ошибок/, { timeout: 60_000 });
  await expect(playground.locator("[data-result]")).toContainText("fuzz: elapsed");
});
