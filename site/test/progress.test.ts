import assert from "node:assert/strict";
import { test } from "node:test";
import {
  checkChoice,
  exportJSON,
  importJSON,
  isComplete,
  type KV,
  lesson,
  load,
  loadDraft,
  recordQuiz,
  recordTask,
  saveDraft,
  setDone,
  solutionUnlocked,
  status,
} from "../src/lib/client/progress.ts";

function memory(): KV & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, v),
    removeItem: (k) => void data.delete(k),
  };
}

const ID = "02-basics/05-slices";

test("битое значение в хранилище даёт пустой прогресс", () => {
  const kv = memory();
  kv.setItem("gotutor:progress", "{not json");
  assert.deepEqual(load(kv), { version: 1, lessons: {} });
});

test("эталон открывается после трёх неудач или успеха", () => {
  const kv = memory();
  recordTask(kv, ID, false);
  recordTask(kv, ID, false);
  assert.equal(solutionUnlocked(lesson(load(kv), ID)), false);
  recordTask(kv, ID, false);
  assert.equal(solutionUnlocked(lesson(load(kv), ID)), true);

  const kv2 = memory();
  recordTask(kv2, ID, true);
  const l = lesson(load(kv2), ID);
  assert.equal(solutionUnlocked(l), true);
  assert.equal(l.task?.attempts, 0, "успех не считается неудачной попыткой");
});

test("урок завершён, когда отвечены все вопросы и решена задача", () => {
  const kv = memory();
  recordQuiz(kv, ID, "a", [0], true);
  assert.equal(isComplete(lesson(load(kv), ID), ["a", "b"], true), false);
  recordQuiz(kv, ID, "b", [1], false);
  assert.equal(isComplete(lesson(load(kv), ID), ["a", "b"], true), false);
  recordTask(kv, ID, true);
  assert.equal(isComplete(lesson(load(kv), ID), ["a", "b"], true), true);
  assert.equal(isComplete(lesson(load(kv), ID), ["a", "b"], false), true);
});

test("checkChoice требует ровно правильный набор", () => {
  assert.equal(checkChoice([1], [1]), true);
  assert.equal(checkChoice([0, 2], [2, 0]), true);
  assert.equal(checkChoice([0], [0, 2]), false);
  assert.equal(checkChoice([0, 1, 2], [0, 2]), false);
});

test("status: none → started → done", () => {
  const kv = memory();
  assert.equal(status(load(kv).lessons[ID]), "none");
  recordQuiz(kv, ID, "a", [0], false);
  assert.equal(status(load(kv).lessons[ID]), "started");
  setDone(kv, ID, true);
  assert.equal(status(load(kv).lessons[ID]), "done");
});

test("импорт сливает прогресс: побеждает более свежая запись", () => {
  const kv = memory();
  setDone(kv, ID, true);
  const other = "05-concurrency/07-select";
  const incoming = {
    version: 1,
    lessons: {
      [ID]: { quiz: {}, done: false, updatedAt: "2000-01-01T00:00:00.000Z" },
      [other]: { quiz: { x: { correct: true, choice: [0] } }, done: true, updatedAt: "2030-01-01T00:00:00.000Z" },
    },
  };
  assert.equal(importJSON(kv, JSON.stringify(incoming)), 1);
  const p = load(kv);
  assert.equal(p.lessons[ID].done, true, "старая запись не перетёрла свежую");
  assert.equal(p.lessons[other].done, true);
  assert.throws(() => importJSON(kv, '{"foo": 1}'), /не файл прогресса/);
  assert.equal(JSON.parse(exportJSON(kv)).version, 1);
});

test("черновик совпадающий с исходником не хранится", () => {
  const kv = memory();
  saveDraft(kv, ID, "task", "changed", "orig");
  assert.equal(loadDraft(kv, ID, "task"), "changed");
  saveDraft(kv, ID, "task", "orig", "orig");
  assert.equal(loadDraft(kv, ID, "task"), null);
  assert.equal(JSON.parse(exportJSON(kv)).lessons[ID], undefined, "черновики не попадают в прогресс");
});
