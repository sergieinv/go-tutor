// Тема сайта: «системная / светлая / тёмная». Выбор хранится в localStorage,
// до отрисовки его ставит на <html data-theme> встроенный скрипт ThemeInit.astro
// (ключ и значения там те же). Системная — атрибута нет, решает prefers-color-scheme.

export type ThemeChoice = "system" | "light" | "dark";

export const THEME_KEY = "gotutor:theme";

export const themeLabels: Record<ThemeChoice, string> = {
  system: "системная",
  light: "светлая",
  dark: "тёмная",
};

const order: ThemeChoice[] = ["system", "light", "dark"];

export const nextChoice = (c: ThemeChoice): ThemeChoice => order[(order.indexOf(c) + 1) % order.length];

export function currentChoice(): ThemeChoice {
  const t = document.documentElement.dataset.theme;
  return t === "light" || t === "dark" ? t : "system";
}

export function setChoice(c: ThemeChoice) {
  const root = document.documentElement;
  if (c === "system") delete root.dataset.theme;
  else root.dataset.theme = c;
  try {
    if (c === "system") localStorage.removeItem(THEME_KEY);
    else localStorage.setItem(THEME_KEY, c);
  } catch {
    // приватный режим, запрет хранилища — выбор действует до перезагрузки
  }
}

/** Тёмная ли тема сейчас — по итоговому color-scheme страницы. */
export const isDark = () => getComputedStyle(document.documentElement).colorScheme === "dark";

/** Вызывает cb при любой смене темы: переключатель или системная настройка. */
export function onThemeChange(cb: () => void) {
  new MutationObserver(cb).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  matchMedia("(prefers-color-scheme: dark)").addEventListener("change", cb);
}
