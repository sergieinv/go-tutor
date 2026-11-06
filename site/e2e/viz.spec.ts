// Критерий этапа 4: оба пилотных визуализатора работают в уроках, шаги листаются
// в обе стороны, живой режим строит схему по реальному запуску; режим слайдов.
import { expect, type Locator, type Page, test } from "@playwright/test";

const counter = (viz: Locator) => viz.locator("[data-counter]");
const next = (viz: Locator) => viz.getByRole("button", { name: "Вперёд →" });
const prev = (viz: Locator) => viz.getByRole("button", { name: "← Назад" });

test("сценарный визуализатор срезов листается в обе стороны", async ({ page }) => {
  await page.goto("/02-basics/05-slices/");
  const viz = page.locator("[data-viz]");
  await expect(counter(viz)).toHaveText("Шаг 1 из 8");
  await expect(prev(viz)).toBeDisabled();
  // Шаг 1: подсвечена строка make, на схеме заголовок s и массив A.
  await expect(viz.locator("[data-code] .line.is-active")).toHaveText("s := make([]int, 3, 4)");
  await expect(viz.locator('svg [data-id="s:box"]')).toHaveCount(1);
  await expect(viz.locator('svg [data-id="B:c0"]')).toHaveCount(0);

  for (let i = 0; i < 5; i++) await next(viz).click();
  await expect(counter(viz)).toHaveText("Шаг 6 из 8");
  await expect(viz.locator("[data-note]")).toContainText("места нет");
  await expect(viz.locator('svg [data-id="B:c0"]')).toHaveCount(1);
  await expect(viz.locator('svg [data-id="u:val:cap"]')).toHaveText("8");

  // Назад: массив B исчезает, подсветка возвращается к строке append(s, 4).
  await prev(viz).click();
  await expect(counter(viz)).toHaveText("Шаг 5 из 8");
  await expect(viz.locator('svg [data-id="B:c0"]')).toHaveCount(0);
  await expect(viz.locator("[data-code] .line.is-active")).toHaveText("s = append(s, 4)");

  // Клавиатура и ползунок.
  await viz.locator("[data-stage]").click();
  await page.keyboard.press("ArrowRight");
  await expect(counter(viz)).toHaveText("Шаг 6 из 8");
  await viz.locator("[data-act=seek]").fill("7");
  await expect(counter(viz)).toHaveText("Шаг 8 из 8");
  await expect(next(viz)).toBeDisabled();
  await viz.getByRole("button", { name: "В начало" }).click();
  await expect(counter(viz)).toHaveText("Шаг 1 из 8");
});

async function buildLive(page: Page, viz: Locator) {
  await viz.getByRole("button", { name: "Запустить и построить" }).click();
  await expect(counter(viz)).toHaveText(/^Шаг 1 из \d+$/, { timeout: 30_000 });
}

test("живой визуализатор select строит шаги по реальному запуску", async ({ page }) => {
  await page.goto("/05-concurrency/07-select/");
  const viz = page.locator("[data-viz]");
  await expect(next(viz)).toBeDisabled();
  await buildLive(page, viz);
  await expect(viz.locator("[data-result]")).toContainText("получено: быстрый");

  // Шаг 2 — запуск первой горутины, подсвечена строка с go в редакторе.
  await next(viz).click();
  await expect(viz.locator("[data-note]")).toContainText("main запускает горутину G2");
  await expect(viz.locator(".cm-viz-line")).toContainText("go func() {");
  await expect(viz.locator('svg [data-id^="g"][data-id$=":name"]')).toHaveText(["main", "G2"]);

  // Где-то дальше main засыпает в select, и его будит G2.
  const total = Number((await counter(viz).textContent())!.match(/из (\d+)/)![1]);
  const notes = await viz.locator("[data-steps-list] li").allTextContents();
  expect(notes).toHaveLength(total);
  expect(notes.some((n) => n.includes("засыпает до готовности"))).toBe(true);
  const wake = notes.findIndex((n) => n.includes("Будит G2"));
  expect(wake).toBeGreaterThan(0);
  await viz.locator("[data-act=seek]").fill(String(wake));
  await expect(viz.locator('svg [data-id="wake"]')).toHaveCount(1);
  await prev(viz).click();
  await expect(viz.locator('svg [data-id="wake"]')).toHaveCount(0);

  // Ученик меняет код: 3 итерации вместо 10 — счётчики последнего select это показывают.
  const editor = viz.locator("[data-editor] .cm-content");
  await editor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText(
    [
      "package main",
      "",
      "func main() {",
      "\ta := make(chan int, 1)",
      "\tb := make(chan int, 1)",
      "\tfor range 3 {",
      "\t\ta <- 1",
      "\t\tb <- 2",
      "\t\tselect {",
      "\t\tcase <-a:",
      "\t\t\t<-b",
      "\t\tcase <-b:",
      "\t\t\t<-a",
      "\t\t}",
      "\t}",
      "}",
      "",
    ].join("\n"),
  );
  await viz.locator("[data-gomaxprocs]").selectOption("1");
  await buildLive(page, viz);
  await viz.locator("[data-act=seek]").press("End");
  const last = await viz.locator("[data-note]").textContent();
  const [, x, y] = last!.match(/case <-a — (\d+), case <-b — (\d+)/)!;
  expect(Number(x) + Number(y)).toBe(3);
  await expect(viz.locator('svg [data-id="s1:title"]')).toHaveText("select · стр. 9");
});

test("ошибка компиляции в живом режиме показывается вместо схемы", async ({ page }) => {
  await page.goto("/05-concurrency/07-select/");
  const viz = page.locator("[data-viz]");
  await viz.locator("[data-editor] .cm-content").click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText("package main\n\nfunc main() {\n\tx := 1\n}\n");
  await viz.getByRole("button", { name: "Запустить и построить" }).click();
  await expect(viz.locator("[data-result]")).toContainText("declared and not used");
  await expect(next(viz)).toBeDisabled();
});

test("слайды: стрелки листают шаги визуализатора, затем слайды", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Слайды модуля" }).first().click();
  await expect(page).toHaveURL(/\/slides\/02-basics\/#1$/);
  const deck = page.locator("[data-deck-counter]");
  await expect(deck).toHaveText(/^1 \/ \d+$/);
  const slides = Number((await deck.textContent())!.split("/ ")[1]);

  // До слайда с визуализатором.
  for (let i = 0; i < 3; i++) await page.keyboard.press("ArrowRight");
  await expect(deck).toHaveText(`4 / ${slides}`);
  const viz = page.locator("[data-slide]:visible [data-viz]");
  await expect(counter(viz)).toHaveText("Шаг 1 из 8");
  await page.keyboard.press("ArrowRight");
  await expect(counter(viz)).toHaveText("Шаг 2 из 8");
  await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("ArrowLeft");
  await expect(deck).toHaveText(`3 / ${slides}`);

  // Вперёд до конца шагов — и дальше на следующий слайд.
  await page.keyboard.press("ArrowRight");
  for (let i = 0; i < 7; i++) await page.keyboard.press("ArrowRight");
  await expect(deck).toHaveText(`4 / ${slides}`);
  await page.keyboard.press("ArrowRight");
  await expect(deck).toHaveText(`5 / ${slides}`);
  await page.reload();
  await expect(deck).toHaveText(`5 / ${slides}`);
});

test("живой визуализатор утечек показывает не завершившиеся горутины", async ({ page }) => {
  await page.goto("/05-concurrency/21-goroutine-leaks/");
  const viz = page.locator("[data-viz]");
  await buildLive(page, viz);
  await viz.locator("[data-act=seek]").press("End");
  await expect(viz.locator("[data-note]")).toContainText("Не завершились к выходу из main");
  await expect(viz.locator('svg [data-id="grp0:box"]')).toHaveCount(1);
  await expect(viz.locator(".cm-viz-line")).not.toHaveCount(0);
});

test("живой визуализатор гонок строит шаги по отчёту -race, после исправления гонок нет", async ({ page }) => {
  await page.goto("/05-concurrency/22-data-races/");
  const viz = page.locator("[data-viz]");
  await buildLive(page, viz);
  await expect(viz.locator("[data-note]")).toContainText("Детектор нашёл");
  await next(viz).click();
  await next(viz).click();
  await next(viz).click();
  await expect(viz.locator('svg [data-id="acc1:box"]')).toHaveCount(1);

  // Программа без гонок — один понятный шаг.
  await viz.locator("[data-editor] .cm-content").click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.insertText('package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("ok")\n}\n');
  await buildLive(page, viz);
  await expect(counter(viz)).toHaveText("Шаг 1 из 1");
  await expect(viz.locator("[data-note]")).toContainText("ничего не нашёл");
});
