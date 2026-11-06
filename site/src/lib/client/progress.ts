// Прогресс ученика в localStorage: ответы квизов, попытки и успех задачи,
// отметка «пройдено». Экспорт/импорт JSON — для переноса между машинами.
// Модуль не зависит от DOM, чтобы его можно было тестировать в Node.

export interface QuizAnswer {
  correct: boolean;
  choice: number[]; // индексы выбранных вариантов
}

export interface TaskProgress {
  attempts: number; // неуспешные проверки
  passed: boolean;
}

export interface LessonProgress {
  quiz: Record<string, QuizAnswer>;
  task?: TaskProgress;
  done: boolean;
  updatedAt: string;
}

export interface Progress {
  version: 1;
  lessons: Record<string, LessonProgress>;
}

/** Минимум от Storage, который нужен модулю. */
export interface KV {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

const KEY = "gotutor:progress";
const DRAFT_PREFIX = "gotutor:draft:";

export const empty = (): Progress => ({ version: 1, lessons: {} });

const emptyLesson = (): LessonProgress => ({ quiz: {}, done: false, updatedAt: "" });

export function load(kv: KV): Progress {
  try {
    const p = JSON.parse(kv.getItem(KEY) ?? "");
    if (p?.version === 1 && typeof p.lessons === "object") return p;
  } catch {
    // пустое или битое значение — начинаем заново
  }
  return empty();
}

export function save(kv: KV, p: Progress) {
  kv.setItem(KEY, JSON.stringify(p));
}

export function lesson(p: Progress, id: string): LessonProgress {
  return p.lessons[id] ?? emptyLesson();
}

/** Применяет изменение к уроку и сохраняет прогресс. */
export function update(kv: KV, id: string, fn: (l: LessonProgress) => void): LessonProgress {
  const p = load(kv);
  const l = structuredClone(lesson(p, id));
  fn(l);
  l.updatedAt = new Date().toISOString();
  p.lessons[id] = l;
  save(kv, p);
  return l;
}

export function recordQuiz(kv: KV, id: string, question: string, choice: number[], correct: boolean) {
  return update(kv, id, (l) => {
    l.quiz[question] = { correct, choice };
  });
}

export function resetQuiz(kv: KV, id: string, question: string) {
  return update(kv, id, (l) => {
    delete l.quiz[question];
  });
}

/** Ответ верен, если выбраны ровно правильные варианты. */
export function checkChoice(choice: number[], correct: number[]): boolean {
  return choice.length === correct.length && correct.every((c) => choice.includes(c));
}

/** Учитывает проверку задачи: успех или ещё одна неудачная попытка. */
export function recordTask(kv: KV, id: string, passed: boolean) {
  return update(kv, id, (l) => {
    const t = l.task ?? { attempts: 0, passed: false };
    if (passed) t.passed = true;
    else t.attempts++;
    l.task = t;
  });
}

export function setDone(kv: KV, id: string, done: boolean) {
  return update(kv, id, (l) => {
    l.done = done;
  });
}

export const SOLUTION_AFTER_ATTEMPTS = 3;

export function solutionUnlocked(l: LessonProgress): boolean {
  return !!l.task && (l.task.passed || l.task.attempts >= SOLUTION_AFTER_ATTEMPTS);
}

/** Урок завершён: все вопросы квиза отвечены, задача решена (если есть). */
export function isComplete(l: LessonProgress, quizIds: string[], hasTask: boolean): boolean {
  const quizDone = quizIds.every((q) => q in l.quiz);
  return quizDone && (!hasTask || !!l.task?.passed);
}

export type Status = "done" | "started" | "none";

export function status(l: LessonProgress | undefined): Status {
  if (!l) return "none";
  if (l.done) return "done";
  return Object.keys(l.quiz).length > 0 || l.task ? "started" : "none";
}

export function exportJSON(kv: KV): string {
  return JSON.stringify(load(kv), null, 2);
}

/**
 * Импортирует прогресс, сливая с текущим: по каждому уроку побеждает
 * более свежая запись. Бросает ошибку на неверном формате.
 */
export function importJSON(kv: KV, json: string): number {
  const incoming = JSON.parse(json);
  if (incoming?.version !== 1 || typeof incoming.lessons !== "object" || incoming.lessons === null) {
    throw new Error("Это не файл прогресса учебника (нужен version: 1 и lessons).");
  }
  const p = load(kv);
  let n = 0;
  for (const [id, l] of Object.entries<LessonProgress>(incoming.lessons)) {
    const cur = p.lessons[id];
    if (!cur || (l.updatedAt ?? "") > (cur.updatedAt ?? "")) {
      p.lessons[id] = { ...emptyLesson(), ...l };
      n++;
    }
  }
  save(kv, p);
  return n;
}

// Черновики кода хранятся отдельно от прогресса и в экспорт не попадают.
export const draftKey = (lessonId: string, slot: string) => `${DRAFT_PREFIX}${lessonId}:${slot}`;

export function loadDraft(kv: KV, lessonId: string, slot: string): string | null {
  return kv.getItem(draftKey(lessonId, slot));
}

export function saveDraft(kv: KV, lessonId: string, slot: string, code: string, original: string) {
  if (code === original) kv.removeItem(draftKey(lessonId, slot));
  else kv.setItem(draftKey(lessonId, slot), code);
}

/** localStorage, если он доступен; иначе — память страницы (приватный режим и т.п.). */
export function storage(): KV {
  try {
    const k = "gotutor:probe";
    localStorage.setItem(k, "1");
    localStorage.removeItem(k);
    return localStorage;
  } catch {
    const m = new Map<string, string>();
    return {
      getItem: (key) => m.get(key) ?? null,
      setItem: (key, v) => void m.set(key, v),
      removeItem: (key) => void m.delete(key),
    };
  }
}

/** Событие, по которому части страницы узнают об изменении прогресса. */
export const CHANGED = "gotutor:progress";
