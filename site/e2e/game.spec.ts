// Этап 13: браузерный клиент игрового сервера (страница /game/).
// Нужен запущенный gameserver профиля labs:
//   docker compose --profile labs up -d gameserver
// Страница должна открываться с разрешённого Origin (порты 8080 или 4321).
import { expect, type Page, test } from "@playwright/test";

// Ники уникальны на прогон: второй вход с тем же ником сервер отклоняет.
const suffix = String(Date.now() % 100000).padStart(5, "0");
const nameA = `e2eA${suffix}`;
const nameB = `e2eB${suffix}`;

async function join(page: Page, name: string) {
  await page.goto("/game/");
  await page.locator("[data-name]").fill(name);
  await page.getByRole("button", { name: "Войти" }).click();
  await expect(page.locator("[data-status]")).toContainText(`В игре: ${name}`, { timeout: 15_000 });
}

const tickNumber = async (page: Page) => Number(await page.locator("[data-tick]").textContent());

test("страница открывается: форма входа, адрес по умолчанию", async ({ page }) => {
  await page.goto("/game/");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Игровой сервер: браузерный клиент");
  await expect(page.locator("[data-url]")).toHaveValue("ws://127.0.0.1:25580/ws");
  await expect(page.locator("[data-game]")).toBeHidden();
});

test("сервер недоступен — понятное сообщение с командой запуска", async ({ page }) => {
  await page.goto("/game/");
  await page.locator("[data-url]").fill("ws://127.0.0.1:9/ws");
  await page.getByRole("button", { name: "Войти" }).click();
  const problem = page.locator("[data-problem]");
  await expect(problem).toBeVisible();
  await expect(problem).toContainText("docker compose --profile labs up -d gameserver");
  await expect(problem.getByRole("button", { name: "Переподключиться" })).toBeVisible();
});

test("два игрока видят друг друга, чат доходит, тики идут", async ({ browser }) => {
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  const a = await ctxA.newPage();
  const b = await ctxB.newPage();
  try {
    await join(a, nameA);
    await join(b, nameB);

    // Список игроков (join) и зона видимости (spawn): оба появляются у спавна.
    for (const [page, other] of [
      [a, nameB],
      [b, nameA],
    ] as const) {
      await expect(page.locator(`[data-roster] li[data-name="${other}"]`)).toBeVisible();
      await expect(page.locator(`[data-visible] li[data-name="${other}"]`)).toBeVisible();
      await expect(page.locator(`[data-roster] li[data-name="${other}"]`)).toHaveAttribute("data-state", "visible");
    }
    await expect(a.locator(`[data-roster] li[data-name="${nameA}"]`)).toHaveAttribute("data-state", "self");

    // Чанки загружены: зона видимости 13×13.
    await expect(a.locator('[data-stat="chunks"]')).toHaveText(/^1[0-9]{2} \/ 169$/, { timeout: 15_000 });

    // Чат: текст с разметкой показывается как текст.
    const text = `привет <b>${suffix}</b>`;
    await a.locator("[data-chat-input]").fill(text);
    await a.getByRole("button", { name: "Отправить" }).click();
    await expect(b.locator("[data-chat-log]")).toContainText(`<${nameA}> ${text}`);
    await expect(b.locator("[data-chat-log] b").filter({ hasText: new RegExp(`^${suffix}$`) })).toHaveCount(0);

    // Счётчик тиков растёт, лента событий показывает spawn.
    const t1 = await tickNumber(a);
    await expect.poll(() => tickNumber(a), { timeout: 5_000 }).toBeGreaterThan(t1 + 5);
    // Лента: переключатель по типам — без chunk и с spawn видно появление B.
    await a.locator('[data-log-filters] input[data-type="chunk"]').uncheck();
    await expect(a.locator("[data-log] .ev-spawn")).toContainText(nameB);

    // Движение: B идёт на восток, у A меняется расстояние до B в зоне видимости.
    const dist = () => b.locator(`[data-visible] li[data-name="${nameA}"] .tag`).textContent();
    const before = await dist();
    await b.locator("[data-map]").focus();
    await b.keyboard.down("KeyD");
    await b.waitForTimeout(800);
    await b.keyboard.up("KeyD");
    await expect.poll(dist).not.toBe(before);

    // Выход B: у A он пропадает из обоих списков.
    await b.getByRole("button", { name: "Выйти" }).click();
    await expect(a.locator(`[data-roster] li[data-name="${nameB}"]`)).toHaveCount(0);
    await expect(a.locator(`[data-visible] li[data-name="${nameB}"]`)).toHaveCount(0);
    await expect(a.locator("[data-chat-log]")).toContainText(`${nameB} left the game`);
  } finally {
    await ctxA.close();
    await ctxB.close();
  }
});
