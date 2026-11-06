// inlineMarkdown: код в одинарных и двойных обратных кавычках, экранирование HTML.
import assert from "node:assert/strict";
import { test } from "node:test";
import { inlineMarkdown } from "../src/lib/markdown.ts";

test("inlineMarkdown: одинарные и двойные обратные кавычки", () => {
  assert.equal(inlineMarkdown("вызов `f(x)` и `<b>`"), "вызов <code>f(x)</code> и <code>&lt;b&gt;</code>");
  assert.equal(inlineMarkdown('поле `` Name string `json:"n"` `` тут'), 'поле <code>Name string `json:&quot;n&quot;`</code> тут');
  assert.equal(inlineMarkdown("без кода"), "без кода");
});

test("директивы //go: в примере — код, а не пояснение", async () => {
  const { segments } = await import("../src/lib/annotate.ts");
  const s = segments('// Файл встраивается при сборке.\n\n//go:embed hello.txt\nvar hello string\n');
  assert.equal(s.length, 1);
  assert.deepEqual(s[0].doc, ["Файл встраивается при сборке."]);
  assert.equal(s[0].code, "//go:embed hello.txt\nvar hello string");
});
