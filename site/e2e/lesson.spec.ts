// Критерий этапа 3: пилотные уроки проходятся от начала до конца —
// песочница, «Поделиться», квиз, задача с попытками, эталон, отметка «пройдено».
import { expect, type Locator, type Page, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import YAML from "yaml";

const content = path.resolve(import.meta.dirname, "../../content");
const read = (lesson: string, file: string) => fs.readFileSync(path.join(content, lesson, file), "utf8");

interface Question {
  id: string;
  kind: string;
  compiles?: boolean;
  options?: { correct?: boolean }[];
}

const correctIndexes = (q: Question): number[] =>
  q.kind === "compile" ? [q.compiles ? 0 : 1] : (q.options ?? []).flatMap((o, i) => (o.correct ? [i] : []));

/** Заменяет весь текст в редакторе CodeMirror. */
async function replaceCode(page: Page, editor: Locator, code: string) {
  await editor.locator(".cm-content").click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(code);
}

const lessons = [
  { id: "02-basics/05-slices", url: "/02-basics/05-slices/" },
  { id: "05-concurrency/07-select", url: "/05-concurrency/07-select/" },
];

for (const { id, url } of lessons) {
  test(`урок ${id} проходится от начала до конца`, async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.goto(url);
    const firstOutputLine = read(id, "example/output.txt").split("\n")[0];

    // Песочница: исходный пример выполняется в runner.
    const playground = page.locator("[data-playground]");
    await playground.getByRole("button", { name: "Запустить" }).click();
    await expect(playground.locator("[data-result]")).toContainText("Готово");
    await expect(playground.locator("[data-result]")).toContainText(firstOutputLine);

    // Формат и ошибка компиляции.
    const editor = playground.locator("[data-editor]");
    await replaceCode(page, editor, 'package main\nimport "fmt"\nfunc main(){\nfmt.Println("e2e")}\n');
    await playground.getByRole("button", { name: "Формат" }).click();
    await expect(editor).toContainText("fmt.Println(\"e2e\")");
    await expect(editor.locator(".cm-line").nth(1)).toHaveText("");
    // Ctrl/Cmd+Enter работает из редактора — после «Формат» фокус на кнопке.
    await editor.locator(".cm-content").click();
    await page.keyboard.press("ControlOrMeta+Enter");
    await expect(playground.locator("[data-result]")).toContainText("e2e");
    await replaceCode(page, editor, "package main\n\nfunc main() {\n\tx := 1\n}\n");
    await playground.getByRole("button", { name: "Запустить" }).click();
    await expect(playground.locator("[data-result]")).toContainText("Ошибка компиляции");
    await expect(playground.locator("[data-result]")).toContainText("declared and not used");

    // «Поделиться»: ссылка с кодом открывает тот же код.
    await replaceCode(page, editor, 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("shared-42")\n}\n');
    await playground.getByRole("button", { name: "Поделиться" }).click();
    await expect(playground.locator("[data-note]")).toContainText("Ссылка с кодом");
    expect(page.url()).toContain("#code=");
    await page.reload();
    await expect(page.locator("[data-playground] [data-editor]")).toContainText("shared-42");
    await expect(page.locator("[data-playground] [data-note]")).toContainText("из ссылки");

    // «Сбросить» возвращает пример.
    await page.locator("[data-playground]").getByRole("button", { name: "Сбросить" }).click();
    await expect(page.locator("[data-playground] [data-editor]")).not.toContainText("shared-42");

    // Квиз: первый вопрос сначала неверно, затем заново и верно; остальные — верно.
    const questions: Question[] = YAML.parse(read(id, "quiz.yaml")).questions;
    for (const [n, q] of questions.entries()) {
      const form = page.locator(`[data-question][data-id="${q.id}"]`);
      const right = correctIndexes(q);
      if (n === 0) {
        const wrong = right[0] === 0 ? 1 : 0;
        await form.locator(`input[value="${wrong}"]`).check();
        await form.getByRole("button", { name: "Ответить" }).click();
        await expect(form.locator("[data-verdict]")).toHaveText(/Неверно/);
        await expect(form.locator(".option.is-correct")).toHaveCount(right.length);
        await form.getByRole("button", { name: "Ответить заново" }).click();
      }
      for (const i of right) await form.locator(`input[value="${i}"]`).check();
      await form.getByRole("button", { name: "Ответить" }).click();
      await expect(form.locator("[data-verdict]")).toHaveText("Верно!");
    }
    await expect(page.locator("[data-quiz] [data-score]")).toHaveText(
      `· верно ${questions.length} из ${questions.length}`,
    );

    // Задача: заготовка не проходит, после трёх неудач открывается эталон.
    const task = page.locator("[data-task]");
    const solutionButton = task.getByRole("button", { name: "Показать эталон" });
    await expect(solutionButton).toBeDisabled();
    for (let attempt = 1; attempt <= 3; attempt++) {
      await task.getByRole("button", { name: "Проверить" }).click();
      await expect(task.locator("[data-result]")).toContainText(/Пройдено \d+ из \d+/);
      await expect(task.locator("[data-state]")).toHaveText(`· попыток: ${attempt}`);
    }
    await expect(task.locator("[data-result] .badge").first()).toHaveText("скрытый");

    // Подсказки открываются по одной.
    for (const n of [1, 2, 3]) await task.getByRole("button", { name: `Показать подсказку ${n}` }).click();
    await expect(task.locator("[data-hint]:visible")).toHaveCount(3);

    await expect(solutionButton).toBeEnabled();
    await solutionButton.click();
    await expect(task.locator("[data-solution]")).toBeVisible();

    // Решение из эталона проходит все тесты, включая скрытые.
    await replaceCode(page, task.locator("[data-editor]"), read(id, "task/solution.go"));
    await task.getByRole("button", { name: "Проверить" }).click();
    await expect(task.locator("[data-result]")).toContainText("Все тесты пройдены");
    await expect(task.locator("[data-state]")).toHaveText("· решено ✓");

    // Урок отмечен пройденным сам, черновик решения пережил перезагрузку.
    const status = page.locator("[data-status]");
    await expect(status).toContainText("Урок пройден ✓");
    await page.reload();
    await expect(page.locator("[data-status]")).toContainText("Урок пройден ✓");
    await expect(page.locator("[data-quiz] [data-score]")).toHaveText(
      `· верно ${questions.length} из ${questions.length}`,
    );

    // Оглавление показывает урок пройденным, экспорт содержит прогресс.
    await page.goto("/");
    await expect(page.locator(`[data-lesson-id="${id}"]`)).toHaveAttribute("data-status", "done");
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: "Экспорт в JSON" }).click();
    const file = await (await download).path();
    const exported = JSON.parse(fs.readFileSync(file, "utf8"));
    expect(exported.lessons[id].done).toBe(true);

    // Импорт в «чистый» браузер восстанавливает прогресс.
    await page.evaluate(() => localStorage.clear());
    await page.reload();
    await expect(page.locator(`[data-lesson-id="${id}"]`)).toHaveAttribute("data-status", "none");
    await page.locator("[data-import]").setInputFiles(file);
    await expect(page.locator("[data-io-note]")).toContainText("Импортировано уроков: 1");
    await expect(page.locator(`[data-lesson-id="${id}"]`)).toHaveAttribute("data-status", "done");
  });
}

test("без runner песочница сообщает, что делать", async ({ page }) => {
  await page.route("**/api/run", (route) => route.fulfill({ status: 502, body: "Bad Gateway" }));
  await page.goto(lessons[0].url);
  await page.locator("[data-playground]").getByRole("button", { name: "Запустить" }).click();
  await expect(page.locator("[data-playground] [data-result]")).toContainText("docker compose up");
});

test("Tab в редакторе ставит отступ, Esc и Tab выводят из редактора", async ({ page }) => {
  await page.goto(lessons[0].url);
  const editor = page.locator("[data-playground] [data-editor]");
  await replaceCode(page, editor, "package main\n\nfunc main() {\nx\n}\n");
  const line = editor.locator(".cm-line").nth(3);
  await line.click();
  await page.keyboard.press("Tab");
  await expect(line).toHaveText("\tx");
  await expect(editor.locator(".cm-content")).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(line).toHaveText("x");

  await page.keyboard.press("Escape");
  await page.keyboard.press("Tab");
  await expect(editor.locator(".cm-content")).not.toBeFocused();
});
