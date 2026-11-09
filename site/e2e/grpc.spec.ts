// Этап 10: контракты .proto — генерация кода при каждом запуске, кнопки
// buf lint / buf breaking, контракт задачи и живой визуализатор spans.
import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const content = path.resolve(import.meta.dirname, "../../content");

async function replaceEditor(page: Page, editor: ReturnType<Page["locator"]>, text: string) {
  await editor.locator(".cm-content").click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(text);
}

test("пример с .proto: код генерируется при запуске, buf lint чистый, удаление поля ломает совместимость", async ({ page }) => {
  const lesson = "11-grpc/05-grpc-unary";
  const want = fs.readFileSync(path.join(content, lesson, "example", "output.txt"), "utf8").trim().split("\n");
  const proto = fs.readFileSync(path.join(content, lesson, "example", "orders.proto"), "utf8");
  await page.goto(`/${lesson}/`);
  const playground = page.locator("[data-playground]");
  const result = playground.locator("[data-result]");

  await playground.getByRole("button", { name: "Запустить" }).click();
  await expect(result).toContainText(want[0], { timeout: 90_000 });
  await expect(result).toContainText(want[want.length - 1]);

  await playground.getByRole("button", { name: "buf lint" }).click();
  await expect(result).toContainText("Нарушений lint нет", { timeout: 60_000 });

  // Удаляем первое поле сообщения: buf breaking (категория FILE) это ловит.
  const field = /^ {2}(?:repeated |optional )?[\w.]+ \w+ = \d+;.*\n/m.exec(proto);
  expect(field, "в контракте нет поля").toBeTruthy();
  await replaceEditor(page, playground.locator('[data-editor="orders.proto"]'), proto.replace(field![0], ""));
  await playground.getByRole("button", { name: "buf breaking" }).click();
  await expect(result).toContainText("Изменения ломают совместимость", { timeout: 60_000 });
  await expect(result).toContainText("FIELD_NO_DELETE");
});

test("пример с намеренными нарушениями: buf lint показывает правила", async ({ page }) => {
  await page.goto("/11-grpc/03-buf/");
  const playground = page.locator("[data-playground]");
  await playground.getByRole("button", { name: "buf lint" }).click();
  const result = playground.locator("[data-result]");
  await expect(result).toContainText("Контракт нарушает правила lint", { timeout: 60_000 });
  await expect(result).toContainText("RPC_REQUEST_STANDARD_NAME");
});

test("задача с контрактом: .proto показан над тестами, эталон проходит", async ({ page }) => {
  const lesson = "11-grpc/14-orders-api";
  await page.goto(`/${lesson}/`);
  const task = page.locator("[data-task]");
  await expect(task.locator("summary", { hasText: "Контракт задачи — orders.proto" })).toBeVisible();
  const solution = fs.readFileSync(path.join(content, lesson, "task", "solution.go"), "utf8");
  await replaceEditor(page, task.locator("[data-editor]").first(), solution);
  await task.getByRole("button", { name: "Проверить" }).click();
  await expect(task.locator("[data-result]")).toContainText(/Все тесты пройдены: (\d+) из \1\b/, { timeout: 120_000 });
});

test("живой визуализатор spans: строит задачи и регионы по runtime/trace", async ({ page }) => {
  await page.goto("/11-grpc/09-interceptors/");
  const live = page.locator('[data-viz][data-kind="live"]');
  await live.getByRole("button", { name: "Запустить и построить" }).click();
  await expect(live.locator("[data-counter]")).toHaveText(/^Шаг 1 из \d+$/, { timeout: 60_000 });
  await live.locator("[data-act=seek]").press("End");
  await expect(live.locator("svg").first()).toBeVisible();
});
