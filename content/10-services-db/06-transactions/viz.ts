// Сценарий «Под капотом»: две транзакции PostgreSQL на одной таблице.
// Read Committed: грязного чтения нет, но есть неповторяемое чтение и фантом.
// Repeatable Read: снимок на всю транзакцию, а конкурентное обновление той же
// строки заканчивается ошибкой 40001 — транзакцию повторяют целиком.
import { box, defineScenario, label, scene, struct } from "../../../viz/src/index.ts";
import type { Shape } from "../../../viz/src/index.ts";

const code = `const q = \`SELECT balance FROM accounts WHERE id = $1\` // id 1: 100, id 2: 50
rc := &sql.TxOptions{Isolation: sql.LevelReadCommitted}
t1, _ := db.BeginTx(ctx, rc)
t2, _ := db.BeginTx(ctx, rc)

t1.QueryRowContext(ctx, q, 1).Scan(&b) // 100
t2.ExecContext(ctx, \`UPDATE accounts SET balance = 70 WHERE id = 1\`)
t1.QueryRowContext(ctx, q, 1).Scan(&b) // 100: черновик T2 не виден
t2.Commit()
t1.QueryRowContext(ctx, q, 1).Scan(&b) // 70: неповторяемое чтение

t1.QueryRowContext(ctx, \`SELECT count(*) FROM accounts\`).Scan(&n) // 2
db.ExecContext(ctx, \`INSERT INTO accounts VALUES (3, 10)\`)
t1.QueryRowContext(ctx, \`SELECT count(*) FROM accounts\`).Scan(&n) // 3: фантом
t1.Commit()

rr := &sql.TxOptions{Isolation: sql.LevelRepeatableRead}
t1, _ = db.BeginTx(ctx, rr)
t2, _ = db.BeginTx(ctx, rr)
t1.QueryRowContext(ctx, q, 1).Scan(&b) // 70: снимок взят
t2.ExecContext(ctx, \`UPDATE accounts SET balance = balance - 20 WHERE id = 1\`)
t2.Commit()
t1.QueryRowContext(ctx, q, 1).Scan(&b) // 70: снимок не меняется
_, err := t1.ExecContext(ctx, \`UPDATE accounts SET balance = balance - 10 WHERE id = 1\`)
fmt.Println(err) // ... (SQLSTATE 40001)
t1.Rollback()

// повтор всей транзакции
t1, _ = db.BeginTx(ctx, rr)
t1.QueryRowContext(ctx, q, 1).Scan(&b) // 50
t1.ExecContext(ctx, \`UPDATE accounts SET balance = balance - 10 WHERE id = 1\`)
t1.Commit() // id 1: 40`;

type TxState = "active" | "committed" | "rolledback" | "none";

interface TxView {
  title: string;
  state: TxState;
  log: string[];
}

interface Frame {
  t1: TxView;
  t2: TxView;
  /** Зафиксированные значения: id 1, 2, 3 (null — строки нет). */
  table: [number, number, number | null];
  /** Незафиксированная версия строки id 1 у T2. */
  draft?: string;
  /** Снимок T1 (Repeatable Read). */
  snapshot?: [number, number, number | null];
  verdict: { title: string; lines: string[]; bad?: boolean };
  hl?: string[];
}

const stateText: Record<TxState, string> = {
  active: "активна",
  committed: "COMMIT",
  rolledback: "ROLLBACK",
  none: "",
};

function txBox(id: string, x: number, v: TxView): Shape[] {
  const status = v.state === "none" ? [] : [`[${stateText[v.state]}]`];
  return box(id, {
    x,
    y: 30,
    w: 220,
    h: 150,
    title: v.title,
    lines: [...status, ...v.log.slice(-5)],
    cls: v.state === "rolledback" ? "v-waiting" : v.state === "committed" || v.state === "none" ? "v-muted-box" : undefined,
  }).shapes;
}

const val = (v: number | null) => (v === null ? "—" : String(v));

function frame(f: Frame): Shape[] {
  const parts: Parameters<typeof scene> = [
    txBox("t1", 10, f.t1),
    txBox("t2", 490, f.t2),
    struct("table", {
      x: 260,
      y: 30,
      w: 200,
      title: "accounts (COMMIT)",
      fields: [
        ["id 1", val(f.table[0])],
        ["id 2", val(f.table[1])],
        ["id 3", val(f.table[2])],
      ],
      hl: f.hl,
    }),
  ];
  if (f.draft) parts.push(box("draft", { x: 260, y: 150, w: 200, h: 70, title: "черновик T2", lines: [f.draft, "другим не виден"], cls: "v-hl" }));
  if (f.snapshot) {
    parts.push(
      struct("snap", {
        x: 10,
        y: 200,
        w: 220,
        title: "снимок T1",
        fields: [
          ["id 1", val(f.snapshot[0])],
          ["id 2", val(f.snapshot[1])],
          ["id 3", val(f.snapshot[2])],
        ],
      }),
    );
  }
  parts.push(box("verdict", { x: 490, y: 200, w: 220, h: 70, title: f.verdict.title, lines: f.verdict.lines, cls: f.verdict.bad ? "v-hl v-waiting" : undefined }));
  parts.push(label("legend", 260, 300, "T1 и T2 — разные соединения пула", "v-caption"));
  return scene(...parts);
}

const RC1 = "T1 · Read Committed";
const RC2 = "T2 · Read Committed";
const RR1 = "T1 · Repeatable Read";
const RR2 = "T2 · Repeatable Read";

export default defineScenario({
  title: "Транзакции и уровни изоляции",
  code,
  width: 720,
  height: 320,
  steps: [
    {
      lines: [2, 3, 4],
      note: "Две транзакции на Read Committed — уровне PostgreSQL по умолчанию. Каждая `BeginTx` берёт из пула своё соединение и держит его до `Commit`/`Rollback`.",
      scene: frame({ t1: { title: RC1, state: "active", log: [] }, t2: { title: RC2, state: "active", log: [] }, table: [100, 50, null], verdict: { title: "пока всё честно", lines: [] } }),
    },
    {
      lines: [6],
      note: "T1 читает баланс счёта 1: 100.",
      scene: frame({ t1: { title: RC1, state: "active", log: ["SELECT id 1 → 100"] }, t2: { title: RC2, state: "active", log: [] }, table: [100, 50, null], verdict: { title: "пока всё честно", lines: [] }, hl: ["id 1"] }),
    },
    {
      lines: [7],
      note: "T2 меняет баланс на 70, но не фиксирует. Новая версия строки существует только внутри T2, а сама строка теперь заблокирована T2 для других изменений до конца транзакции.",
      scene: frame({ t1: { title: RC1, state: "active", log: ["SELECT id 1 → 100"] }, t2: { title: RC2, state: "active", log: ["UPDATE id 1 = 70"] }, table: [100, 50, null], draft: "id 1 → 70", verdict: { title: "пока всё честно", lines: [] } }),
    },
    {
      lines: [8],
      note: "T1 читает снова и видит 100: незафиксированные изменения чужой транзакции не видны. Это защита от грязного чтения. В PostgreSQL его не бывает вообще — даже `LevelReadUncommitted` работает как Read Committed. SQL Server и MySQL на READ UNCOMMITTED вернули бы 70.",
      scene: frame({ t1: { title: RC1, state: "active", log: ["SELECT id 1 → 100", "SELECT id 1 → 100"] }, t2: { title: RC2, state: "active", log: ["UPDATE id 1 = 70"] }, table: [100, 50, null], draft: "id 1 → 70", verdict: { title: "грязного чтения нет", lines: ["черновик T2 не виден"] } }),
    },
    {
      lines: [9],
      note: "T2 фиксируется: 70 становится зафиксированным значением, блокировка строки снята.",
      scene: frame({ t1: { title: RC1, state: "active", log: ["SELECT id 1 → 100", "SELECT id 1 → 100"] }, t2: { title: RC2, state: "committed", log: ["UPDATE id 1 = 70"] }, table: [70, 50, null], verdict: { title: "грязного чтения нет", lines: ["черновик T2 не виден"] }, hl: ["id 1"] }),
    },
    {
      lines: [10],
      note: "Третье чтение в той же T1 даёт 70. Read Committed берёт свежий снимок на каждый запрос, поэтому одна транзакция видит разные значения одной строки — неповторяемое чтение. Если T1 считала по первому значению, её расчёт устарел.",
      scene: frame({ t1: { title: RC1, state: "active", log: ["SELECT id 1 → 100", "SELECT id 1 → 100", "SELECT id 1 → 70  (!)"] }, t2: { title: RC2, state: "committed", log: ["UPDATE id 1 = 70"] }, table: [70, 50, null], verdict: { title: "неповторяемое чтение", lines: ["100, потом 70"], bad: true }, hl: ["id 1"] }),
    },
    {
      lines: [12, 13, 14],
      note: "То же со множеством строк: T1 считает счета — 2, другая сессия вставляет и фиксирует строку, повторный подсчёт даёт 3. Появившаяся строка — фантом.",
      scene: frame({ t1: { title: RC1, state: "active", log: ["SELECT id 1 → 70  (!)", "count(*) → 2", "count(*) → 3  (!)"] }, t2: { title: "db (автокоммит)", state: "none", log: ["INSERT (3, 10)", "зафиксировано сразу"] }, table: [70, 50, 10], verdict: { title: "фантом", lines: ["2 строки, потом 3"], bad: true }, hl: ["id 3"] }),
    },
    {
      lines: [17, 18, 19, 20],
      note: "Теперь Repeatable Read. Снимок данных T1 берётся на первом запросе и живёт до конца транзакции: T1 читает 70.",
      scene: frame({ t1: { title: RR1, state: "active", log: ["SELECT id 1 → 70"] }, t2: { title: RR2, state: "active", log: [] }, table: [70, 50, 10], snapshot: [70, 50, 10], verdict: { title: "снимок взят", lines: ["на первом запросе T1"] } }),
    },
    {
      lines: [21, 22],
      note: "T2 списывает 20 со счёта 1 и фиксируется: в таблице 50.",
      scene: frame({ t1: { title: RR1, state: "active", log: ["SELECT id 1 → 70"] }, t2: { title: RR2, state: "committed", log: ["UPDATE id 1 = 70 - 20"] }, table: [50, 50, 10], snapshot: [70, 50, 10], verdict: { title: "снимок взят", lines: ["на первом запросе T1"] }, hl: ["id 1"] }),
    },
    {
      lines: [23],
      note: "T1 по-прежнему видит 70: она читает из своего снимка. Неповторяемого чтения и фантомов на этом уровне в PostgreSQL нет.",
      scene: frame({ t1: { title: RR1, state: "active", log: ["SELECT id 1 → 70", "SELECT id 1 → 70"] }, t2: { title: RR2, state: "committed", log: ["UPDATE id 1 = 70 - 20"] }, table: [50, 50, 10], snapshot: [70, 50, 10], verdict: { title: "чтение повторяемо", lines: ["T1 видит свой снимок"] } }),
    },
    {
      lines: [24, 25, 26],
      note: "T1 пытается списать 10. Строку после снимка T1 изменила зафиксированная T2: запиши PostgreSQL `70 - 10 = 60`, списание T2 бы потерялось. Вместо этого запрос падает с `could not serialize access due to concurrent update` (SQLSTATE `40001`), и транзакцию остаётся только откатить.",
      scene: frame({ t1: { title: RR1, state: "rolledback", log: ["SELECT id 1 → 70", "SELECT id 1 → 70", "UPDATE → ошибка 40001"] }, t2: { title: RR2, state: "committed", log: ["UPDATE id 1 = 70 - 20"] }, table: [50, 50, 10], snapshot: [70, 50, 10], verdict: { title: "конфликт: 40001", lines: ["обновление не потеряно"], bad: true } }),
    },
    {
      lines: [28, 29, 30, 31, 32],
      note: "Ошибку `40001` лечат повтором всей транзакции с начала: новая T1 берёт свежий снимок, читает 50 и записывает 40. Повторять один упавший запрос бессмысленно — решение принималось по устаревшему снимку. Цикл повторов — в уроках про блокировки и про изоляцию в PostgreSQL.",
      scene: frame({ t1: { title: RR1, state: "committed", log: ["(повтор)", "SELECT id 1 → 50", "UPDATE id 1 = 50 - 10"] }, t2: { title: RR2, state: "committed", log: ["UPDATE id 1 = 70 - 20"] }, table: [40, 50, 10], snapshot: [50, 50, 10], verdict: { title: "повтор удался", lines: ["оба списания учтены"] }, hl: ["id 1"] }),
    },
  ],
});
