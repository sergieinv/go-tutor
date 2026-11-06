import assert from "node:assert/strict";
import { test } from "node:test";
import { decode, encode, fromHash, shareURL } from "../src/lib/client/share.ts";

const code = 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("привет, </script> & #")\n}\n';

test("encode/decode сохраняют код, включая кириллицу", async () => {
  assert.equal(await decode(await encode(code)), code);
});

test("ссылка: код во фрагменте и читается обратно", async () => {
  const url = await shareURL(code, "http://localhost:8080/02-basics/05-slices/#old");
  const u = new URL(url);
  assert.equal(u.pathname, "/02-basics/05-slices/");
  assert.match(u.hash, /^#code=[A-Za-z0-9_-]+$/);
  assert.equal(await fromHash(u.hash), code);
});

test("мусор во фрагменте не ломает страницу", async () => {
  assert.equal(await fromHash(""), null);
  assert.equal(await fromHash("#code=@@@"), null);
  assert.equal(await fromHash("#quiz-1"), null);
});
