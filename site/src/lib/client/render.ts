// Отрисовка результатов runner. Вывод программ — только через textContent.
import type { GoTestMode, GoTestResult, ProfileResult, RunResult, TestResult, VulncheckResult, BufResult } from "./api.ts";

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

/** Проверенный вывод примера из урока (example/output.txt) — на случай, когда runner недоступен. */
export interface SavedOutput {
  /** Подпись блока: команда, чей вывод сохранён. */
  command: string;
  text: string;
  /** Оговорки: чего в сохранённом выводе нет. */
  notes: string[];
}

/** Абзац с текстом, где `код` в обратных кавычках — <code>. */
function para(text: string, className = "muted") {
  const p = el("p", className);
  for (const [i, part] of text.split("`").entries()) p.append(i % 2 ? el("code", undefined, part) : document.createTextNode(part));
  return p;
}

const howToRun = "Чтобы код выполнялся, запустите учебник с песочницей: `docker compose up` (для разработки — `go run ./runner`).";

/** Runner недоступен — вместо запуска показан сохранённый при проверке урока вывод. */
export function showSaved(target: HTMLElement, saved: SavedOutput, title = "Runner недоступен — показан проверенный вывод примера") {
  target.removeAttribute("aria-busy");
  const parts: HTMLElement[] = [status(title, "warn"), para(`Код сейчас не выполнялся: вывод сохранён при проверке урока. ${howToRun}`)];
  for (const n of saved.notes) parts.push(para(n));
  parts.push(saved.text ? block(saved.command, saved.text, "out") : el("p", "muted", "Пример ничего не выводит в stdout."));
  target.replaceChildren(...parts);
}

/**
 * Runner недоступен, а сохранённый вывод не подходит: код изменён или режим
 * (бенчмарки, покрытие, govulncheck…) без сохранённого вывода. offer — кнопка,
 * которая всё же покажет вывод исходного примера.
 */
export function showUnavailable(target: HTMLElement, message: string, detail?: string, offer?: { label: string; show: () => void }) {
  target.removeAttribute("aria-busy");
  const parts: HTMLElement[] = [status(message, "bad")];
  if (detail) parts.push(para(detail));
  if (offer) {
    const b = el("button", undefined, offer.label);
    b.type = "button";
    b.addEventListener("click", offer.show);
    parts.push(b);
  }
  target.replaceChildren(...parts);
}

export function showRun(target: HTMLElement, r: RunResult, gcflags?: string) {
  target.removeAttribute("aria-busy");
  if (r.buildFailed) {
    target.replaceChildren(status("Ошибка компиляции", "bad"), block("go build", r.buildOutput ?? "", "err"));
    return;
  }
  const parts = runParts(r);
  if (gcflags) {
    parts.push(
      r.compilerOutput
        ? block(`go build -gcflags="${gcflags}"`, r.compilerOutput, "out")
        : el("p", "muted", `Компилятор с -gcflags="${gcflags}" ничего не сообщил.`),
    );
  }
  target.replaceChildren(...parts);
}

/** Запуск примера, затем pprof -top по каждому профилю, который он записал. */
export function showProfile(target: HTMLElement, r: ProfileResult) {
  target.removeAttribute("aria-busy");
  if (r.buildFailed) {
    target.replaceChildren(status("Ошибка компиляции", "bad"), block("go build", r.buildOutput ?? "", "err"));
    return;
  }
  const parts = runParts(r);
  for (const p of r.profiles) parts.push(block(`go tool pprof -top ${p.name}`, p.top, p.failed ? "err" : "out"));
  if (r.profiles.length === 0) {
    parts.push(el("p", "muted", "Программа не записала ни одного профиля: pprof ищет файлы *.pprof в рабочем каталоге."));
  }
  target.replaceChildren(...parts);
}

function runParts(r: RunResult): HTMLElement[] {
  const parts: HTMLElement[] = [];
  if (r.timedOut) parts.push(status("Программа прервана: превышен лимит времени", "bad", r.durationMs));
  else if (r.truncated) parts.push(status("Программа прервана: слишком большой вывод (лимит 64 КБ)", "bad", r.durationMs));
  else if (r.exitCode !== 0) parts.push(status(`Программа завершилась с кодом ${r.exitCode}`, "bad", r.durationMs));
  else parts.push(status("Готово", "ok", r.durationMs));
  if (r.stdout) parts.push(block("stdout", r.stdout, "out"));
  if (r.stderr) parts.push(block("stderr", r.stderr, "err"));
  if (!r.stdout && !r.stderr) parts.push(el("p", "muted", "Программа ничего не вывела."));
  return parts;
}

/** Результат go test для примера: вывод как в терминале плюс покрытие или найденные фаззером входы. */
export function showGoTest(target: HTMLElement, r: GoTestResult, mode: GoTestMode) {
  target.removeAttribute("aria-busy");
  if (r.buildFailed) {
    target.replaceChildren(status("Ошибка компиляции", "bad"), block("go test", r.buildOutput ?? "", "err"));
    return;
  }
  const parts: HTMLElement[] = [];
  const found = r.fuzzInputs?.length ?? 0;
  if (r.timedOut) parts.push(status("Прервано: превышен лимит времени", "bad", r.durationMs));
  else if (r.truncated) parts.push(status("Прервано: слишком большой вывод (лимит 64 КБ)", "bad", r.durationMs));
  else if (mode === "fuzz" && found > 0) parts.push(status("Фаззер нашёл вход, на котором тест падает", "bad", r.durationMs));
  else if (r.exitCode !== 0) parts.push(status(mode === "bench" ? "Бенчмарк упал" : "Тесты не прошли", "bad", r.durationMs));
  else {
    const ok: Record<GoTestMode, string> = {
      test: "Тесты пройдены",
      bench: "Бенчмарки выполнены",
      fuzz: "Fuzzing не нашёл ошибок",
      cover: "Тесты пройдены, покрытие посчитано",
    };
    parts.push(status(ok[mode], "ok", r.durationMs));
  }
  const cmd: Record<GoTestMode, string> = {
    test: "go test -v",
    bench: "go test -bench=. -benchmem",
    fuzz: "go test -fuzz",
    cover: "go test -v -cover",
  };
  if (r.stdout) parts.push(block(cmd[mode], r.stdout, "out"));
  if (r.coverage) parts.push(block("go tool cover -func", r.coverage, "out"));
  for (const f of r.fuzzInputs ?? []) parts.push(block(f.name, f.data, "err"));
  if (r.stderr) parts.push(block("stderr", r.stderr, "err"));
  target.replaceChildren(...parts);
}

/** Отчёт govulncheck: найдены ли уязвимые вызовы и дата снимка базы. */
export function showVulncheck(target: HTMLElement, r: VulncheckResult) {
  target.removeAttribute("aria-busy");
  const parts: HTMLElement[] = [];
  if (r.timedOut) parts.push(status("govulncheck не уложился в лимит времени", "bad", r.durationMs));
  else if (r.failed) parts.push(status("govulncheck не смог проверить код", "bad", r.durationMs));
  else if (r.found) parts.push(status("Код вызывает уязвимые функции", "bad", r.durationMs));
  else parts.push(status("Уязвимых вызовов не найдено", "ok", r.durationMs));
  if (r.output) parts.push(block("govulncheck ./...", r.output, r.failed ? "err" : "out"));
  if (r.dbModified) parts.push(el("p", "muted", `База уязвимостей — снимок vuln.go.dev от ${r.dbModified}.`));
  target.replaceChildren(...parts);
}

export function showBuf(target: HTMLElement, r: BufResult, breaking: boolean) {
  target.removeAttribute("aria-busy");
  const cmd = breaking ? "buf breaking" : "buf lint";
  const parts: HTMLElement[] = [];
  if (r.timedOut) parts.push(status(`${cmd} не уложился в лимит времени`, "bad", r.durationMs));
  else if (r.failed) parts.push(status(`${cmd} не смог проверить контракт`, "bad", r.durationMs));
  else if (r.found) parts.push(status(breaking ? "Изменения ломают совместимость с исходным контрактом" : "Контракт нарушает правила lint", "bad", r.durationMs));
  else parts.push(status(breaking ? "Совместимо с исходным контрактом" : "Нарушений lint нет", "ok", r.durationMs));
  if (r.output) parts.push(block(cmd, r.output, r.failed ? "err" : "out"));
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
    if (t.hidden) head.append(el("span", "badge", t.name.startsWith("TestExploit") ? "эксплойт" : t.name.startsWith("TestReview") ? "ревью" : "скрытый"));
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
