// Отрисовка результатов runner. Вывод программ — только через textContent.
import type { RunResult, TestResult } from "./api.ts";

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string, text?: string) {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined) e.textContent = text;
  return e;
}

function block(label: string, text: string, kind: "out" | "err") {
  const wrap = el("div", `stream stream-${kind}`);
  wrap.append(el("div", "stream-label", label), el("pre", "plain", text));
  return wrap;
}

/** Строка-сводка: статус + время. */
function status(text: string, kind: "ok" | "bad" | "warn", ms?: number) {
  const s = el("p", `status status-${kind}`, text);
  if (ms !== undefined) s.append(el("span", "muted", ` · ${ms} мс`));
  return s;
}

export function showPending(target: HTMLElement, text: string) {
  target.replaceChildren(status(text, "warn"));
  target.setAttribute("aria-busy", "true");
}

export function showError(target: HTMLElement, message: string) {
  target.replaceChildren(status(message, "bad"));
  target.removeAttribute("aria-busy");
}

export function showRun(target: HTMLElement, r: RunResult) {
  target.removeAttribute("aria-busy");
  if (r.buildFailed) {
    target.replaceChildren(status("Ошибка компиляции", "bad"), block("go build", r.buildOutput ?? "", "err"));
    return;
  }
  const parts: HTMLElement[] = [];
  if (r.timedOut) parts.push(status("Программа прервана: превышен лимит времени", "bad", r.durationMs));
  else if (r.truncated) parts.push(status("Программа прервана: слишком большой вывод (лимит 64 КБ)", "bad", r.durationMs));
  else if (r.exitCode !== 0) parts.push(status(`Программа завершилась с кодом ${r.exitCode}`, "bad", r.durationMs));
  else parts.push(status("Готово", "ok", r.durationMs));
  if (r.stdout) parts.push(block("stdout", r.stdout, "out"));
  if (r.stderr) parts.push(block("stderr", r.stderr, "err"));
  if (!r.stdout && !r.stderr) parts.push(el("p", "muted", "Программа ничего не вывела."));
  target.replaceChildren(...parts);
}

export function showTests(target: HTMLElement, r: TestResult) {
  target.removeAttribute("aria-busy");
  if (r.buildFailed) {
    target.replaceChildren(
      status("Решение не компилируется вместе с тестами", "bad"),
      block("go test", r.buildOutput ?? "", "err"),
    );
    return;
  }
  // Подтесты показываем внутри родителя, сверху — открытые тесты.
  const top = r.tests.filter((t) => !t.name.includes("/"));
  top.sort((a, b) => Number(!!a.hidden) - Number(!!b.hidden));
  const passed = top.filter((t) => t.status === "pass").length;

  const parts: HTMLElement[] = [];
  if (r.passed) parts.push(status(`Все тесты пройдены: ${passed} из ${top.length}`, "ok", r.durationMs));
  else if (r.timedOut) parts.push(status("Тесты прерваны: превышен лимит времени", "bad", r.durationMs));
  else parts.push(status(`Пройдено ${passed} из ${top.length}`, "bad", r.durationMs));

  const list = el("ul", "tests");
  for (const t of top) {
    const li = el("li", `test test-${t.status}`);
    const head = el("div", "test-head");
    head.append(el("span", "test-mark", t.status === "pass" ? "✓" : t.status === "skip" ? "–" : "✗"));
    head.append(el("code", undefined, t.name));
    if (t.hidden) head.append(el("span", "badge", "скрытый"));
    li.append(head);
    const subs = r.tests.filter((s) => s.name.startsWith(t.name + "/") && s.status === "fail");
    const output = [t.output, ...subs.map((s) => `${s.name}\n${s.output}`)].join("").trim();
    if (t.status === "fail" && output) li.append(el("pre", "plain", output));
    list.append(li);
  }
  parts.push(list);
  const extra = packageOutput(r.output ?? "");
  if (!r.passed && extra) parts.push(block("вывод вне тестов", extra, "err"));
  target.replaceChildren(...parts);
}

/** Вывод пакета без итоговых строк go test (PASS, FAIL, ok …) — они дублируют сводку. */
export function packageOutput(output: string): string {
  return output
    .split("\n")
    .filter((l) => !/^(PASS|FAIL|ok\s|FAIL\s|exit status \d+)$/.test(l.trim()) && !/^(ok|FAIL)\s+\S+\s+[\d.]+s/.test(l.trim()))
    .join("\n")
    .trim();
}
