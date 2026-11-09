// Браузерный клиент игрового сервера: разбор протокола шлюза и состояние
// клиента (labs/gameserver, README — «Протокол WebSocket»).
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  chatMsg,
  cleanChat,
  moveMsg,
  parseBatch,
  parseEvent,
  validName,
  yawOf,
} from "../src/lib/client/game-protocol.ts";
import { FLASH_TICKS, GameState, HISTORY, step, Throttle } from "../src/lib/client/game-state.ts";

const welcome = {
  t: "welcome",
  id: 7,
  name: "Alice",
  uuid: "9eb394b9-213f-359e-89ea-3da8f20019bc",
  x: 0.5,
  y: -60,
  z: -7.5,
  yaw: 0,
  pitch: 0,
  tp: 1,
  viewDistance: 6,
  tickRate: 20,
  maxPlayers: 256,
  maxChat: 256,
  world: { chunkSize: 16, minY: -64, height: 384, groundY: -61, borderRadius: 512, spawn: { x: 0.5, y: -60, z: 0.5 } },
  placeable: ["minecraft:stone", "minecraft:glass"],
};

const tick = (n: number, extra: Record<string, number> = {}) => ({ t: "tick", n, mspt: 0.1, tps: 20, msgs: 1, bytes: 10, ...extra });

function chunk(x: number, z: number) {
  return {
    t: "chunk",
    x,
    z,
    palette: ["minecraft:stone", "minecraft:grass_block"],
    top: Array.from({ length: 256 }, (_, i) => (i % 16 === 0 || i < 16 ? 0 : 1)),
    height: Array.from({ length: 256 }, () => -61),
  };
}

const batch = (...evs: unknown[]) => parseBatch(JSON.stringify(evs));

test("parseBatch: невалидный JSON, не массив, не строка — пустая пачка", () => {
  assert.deepEqual(parseBatch("{oops"), []);
  assert.deepEqual(parseBatch('{"t":"tick","n":1}'), []);
  assert.deepEqual(parseBatch(42), []);
  assert.deepEqual(parseBatch("x".repeat(5 << 20)), []);
});

test("parseBatch: неизвестные типы и битые события пропускаются, остальные остаются", () => {
  const evs = batch(
    { t: "future_event", foo: 1 },
    { t: "move", id: "5", x: 1, y: -60, z: 2 }, // id не число
    { t: "move", id: 5, x: 1, y: -60, z: 2, yaw: 90 },
    null,
    [1, 2],
    { t: "chunk", x: 0, z: 0, palette: ["minecraft:stone"], top: [0], height: [0] }, // мало колонок
    { t: "chunk", ...chunk(0, 0), top: Array(256).fill(5) }, // индекс вне палитры
    tick(10),
  );
  assert.deepEqual(
    evs.map((e) => e.t),
    ["move", "tick"],
  );
  assert.equal((evs[0] as { yaw: number }).yaw, 90);
});

test("parseEvent: NaN/Infinity и огромные координаты отбрасываются", () => {
  assert.equal(parseEvent({ t: "move", id: 1, x: 1e300, y: 0, z: 0 }), null);
  assert.equal(parseEvent({ t: "center", x: 1.5, z: 0 }), null, "чанк — целое");
  assert.equal(parseEvent({ t: "chat", id: 1, name: "a".repeat(100), text: "hi" }), null);
  assert.equal(parseEvent({ t: "welcome", ...welcome, world: { ...welcome.world, chunkSize: 32 } }), null);
  assert.ok(parseEvent(welcome));
  assert.ok(parseEvent(chunk(-3, 4)));
});

test("parseEvent: tick без статистики даёт нули, а не undefined", () => {
  assert.deepEqual(parseEvent({ t: "tick", n: 5 }), { t: "tick", n: 5, mspt: 0, tps: 0, msgs: 0, bytes: 0 });
  assert.equal(parseEvent({ t: "tick", n: -1 }), null);
});

test("welcome и teleport требуют tpack; до подтверждения двигаться нельзя", () => {
  const s = new GameState();
  const fx = s.apply(batch(welcome, { t: "join", id: 7, name: "Alice", uuid: "", kind: "web" }, tick(100)));
  assert.deepEqual(fx.tpacks, [1]);
  assert.equal(fx.repositioned, true);
  assert.equal(s.canMove(), false);
  s.acked(1);
  assert.equal(s.canMove(), true);
  assert.equal(s.me?.name, "Alice");
  assert.equal(s.roster.size, 1);

  const fx2 = s.apply(batch({ t: "teleport", tp: 2, x: 3, y: -60, z: 4, yaw: 0, pitch: 0, reason: "too_fast" }, tick(101)));
  assert.deepEqual(fx2.tpacks, [2]);
  assert.equal(s.canMove(), false);
  assert.deepEqual([s.me?.x, s.me?.z], [3, 4]);
  assert.deepEqual(s.lastTeleport, { reason: "too_fast", tick: 101 });
  assert.match(s.chat.at(-1)!.text, /слишком быстро/);
  s.acked(1); // старый номер не снимает ожидание
  assert.equal(s.canMove(), false);
  s.acked(2);
  assert.equal(s.canMove(), true);
});

test("interest management: список игроков (join/leave) отдельно от видимых (spawn/despawn)", () => {
  const s = new GameState();
  s.apply(
    batch(
      welcome,
      { t: "join", id: 7, name: "Alice", kind: "web" },
      { t: "join", id: 8, name: "Bob", kind: "minecraft" },
      { t: "join", id: 9, name: "Carol", kind: "web" },
      { t: "spawn", id: 8, name: "Bob", kind: "minecraft", x: 5, y: -60, z: 5, yaw: 0, pitch: 0 },
      tick(1),
    ),
  );
  assert.deepEqual([...s.roster.keys()], [7, 8, 9]);
  assert.deepEqual([...s.visible.keys()], [8]);

  s.apply(batch({ t: "move", id: 8, x: 6, y: -60, z: 7, yaw: 90, pitch: 0 }, { t: "move", id: 9, x: 1, y: -60, z: 1 }, tick(2)));
  assert.deepEqual([s.visible.get(8)?.x, s.visible.get(8)?.z], [6, 7]);
  assert.equal(s.visible.has(9), false, "move невидимого игрока не создаёт его");

  s.apply(batch({ t: "despawn", id: 8 }, tick(3)));
  assert.equal(s.visible.size, 0);
  assert.equal(s.roster.size, 3);

  s.apply(batch({ t: "spawn", id: 9, name: "Carol", kind: "web", x: 1, y: -60, z: 1 }, { t: "leave", id: 9, name: "Carol" }, tick(4)));
  assert.equal(s.visible.has(9), false, "leave убирает и из видимых");
  assert.equal(s.roster.has(9), false);
});

test("чанки: загрузка, выгрузка, вспышки в тике и статистика тика", () => {
  const s = new GameState();
  s.apply(batch(welcome, { t: "center", x: 0, z: -1 }, chunk(0, -1), chunk(1, -1), tick(50, { msgs: 4 })));
  assert.equal(s.chunks.size, 2);
  assert.deepEqual(s.center, { x: 0, z: -1 });
  assert.equal(s.flashes.length, 2);
  assert.ok(s.flashes.every((f) => f.kind === "load" && f.tick === 50));
  assert.deepEqual(s.ticks.at(-1), { n: 50, mspt: 0.1, tps: 20, msgs: 4, bytes: 10, loaded: 2, unloaded: 0 });
  assert.equal(s.inView(6, 5), true);
  assert.equal(s.inView(7, 5), false);

  s.apply(batch({ t: "unchunk", x: 1, z: -1 }, tick(51)));
  assert.equal(s.chunks.has("1,-1"), false);
  assert.equal(s.ticks.at(-1)?.unloaded, 1);
  assert.equal(s.flashes.length, 3);

  // Через FLASH_TICKS тиков вспышки гаснут.
  s.apply(batch(tick(51 + FLASH_TICKS)));
  assert.equal(s.flashes.length, 0);
});

test("block: новый верх колонки попадает в чанк, палитра дополняется", () => {
  const s = new GameState();
  s.apply(batch(welcome, chunk(-1, -1), tick(1)));
  const v = s.chunks.get("-1,-1")!.version;
  s.apply(batch({ t: "block", x: -3, y: -60, z: -5, block: "minecraft:glass", topY: -60, top: "minecraft:glass" }, tick(2)));
  assert.deepEqual(s.topAt(-3, -5), { block: "minecraft:glass", y: -60 });
  assert.equal(s.chunks.get("-1,-1")!.version, v + 1);
  // Блок в незагруженном чанке игнорируется.
  s.apply(batch({ t: "block", x: 100, y: -60, z: 100, block: "minecraft:glass", topY: -60, top: "minecraft:glass" }, tick(3)));
  assert.equal(s.topAt(100, 100), null);
});

test("история тиков ограничена, лента событий без tick", () => {
  const s = new GameState();
  for (let i = 0; i < HISTORY + 20; i++) s.apply(batch({ t: "system", text: `m${i}` }, tick(i)));
  assert.equal(s.ticks.length, HISTORY);
  assert.equal(s.ticks[0].n, 20);
  assert.equal(s.tick, HISTORY + 19);
  assert.equal(s.log.has("tick"), false);
  assert.equal(s.log.get("system")?.length, 100);
  assert.equal(s.counts.get("tick"), HISTORY + 20);
});

test("лента: частые move не вытесняют редкие spawn, порядок — по времени", () => {
  const s = new GameState();
  s.apply(batch(welcome, { t: "spawn", id: 8, name: "Bob", kind: "web", x: 1, y: -60, z: 1 }, tick(1)));
  for (let i = 0; i < 500; i++) s.apply(batch({ t: "move", id: 8, x: i, y: -60, z: 1 }, tick(2 + i)));
  const r = s.recent(new Set(["spawn", "move"] as const), 3);
  assert.deepEqual(
    r.map((e) => e.t),
    ["move", "move", "move"],
  );
  assert.equal(s.recent(new Set(["spawn"] as const), 10)[0].text, "Bob (id 8) @ 1, 1");
  assert.equal(s.recent(new Set(["move"] as const), 1000).length, 100);
});

test("kick: причина сохраняется, двигаться нельзя", () => {
  const s = new GameState();
  s.apply(batch(welcome, tick(1)));
  s.acked(1);
  const fx = s.apply(batch({ t: "kick", reason: "too many messages" }));
  assert.equal(fx.kicked, "too many messages");
  assert.equal(s.kicked, "too many messages");
  assert.equal(s.canMove(), false);
});

test("чат: текст хранится как есть (экранирование — дело DOM через textContent)", () => {
  const s = new GameState();
  s.apply(batch(welcome, { t: "chat", id: 7, name: "Alice", text: "<img src=x onerror=alert(1)>" }, tick(1)));
  assert.deepEqual(s.chat.at(-1), { kind: "chat", name: "Alice", text: "<img src=x onerror=alert(1)>", self: true });
});

test("сообщения клиента и проверки", () => {
  assert.equal(validName("Alice_01"), true);
  assert.equal(validName("ab"), false);
  assert.equal(validName("Алиса"), false);
  assert.equal(validName("a".repeat(17)), false);
  assert.equal(moveMsg(1.23456, -2, 90), '{"t":"move","x":1.235,"z":-2,"yaw":90}');
  assert.equal(cleanChat("  при\u0007вет §c   "), "привет c");
  assert.equal([...cleanChat("я".repeat(300))].length, 256);
  assert.equal(chatMsg('a"b'), '{"t":"chat","text":"a\\"b"}');
  // yaw как в Minecraft: 0 = +Z, 90 = −X, 180 = −Z, 270 = +X.
  assert.equal(yawOf(0, 1), 0);
  assert.equal(yawOf(-1, 0), 90);
  assert.equal(yawOf(0, -1), 180);
  assert.equal(yawOf(1, 0), 270);
});

test("Throttle и step: частота и скорость в пределах сервера", () => {
  const t = new Throttle(50);
  assert.equal(t.take(0), true);
  assert.equal(t.take(30), false);
  assert.equal(t.take(50), true);

  const p = step({ x: 0, z: 0 }, 1, 0, 10, 0.1, 512);
  assert.deepEqual(p, { x: 1, z: 0 });
  const far = step({ x: 0, z: 0 }, 0, 1, 10, 5, 512);
  assert.equal(far.z, 2.5, "после паузы вкладки — не больше 0.25 с пути");
  const edge = step({ x: 511, z: 0 }, 1, 0, 30, 0.2, 512);
  assert.equal(edge.x, 511.5, "у границы мира останавливаемся внутри");
});
