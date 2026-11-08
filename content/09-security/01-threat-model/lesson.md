---
title: Модель угроз и контроль доступа
summary: Активы, границы доверия и STRIDE, OWASP Top 10:2025 и API Security Top 10:2023 — карта модуля, broken access control на примере IDOR и проверка владельца в хендлере.
difficulty: 3
minutes: 35
prerequisites:
  - 08-web/04-middleware
  - 08-web/05-json-api
  - 08-web/07-request-context
docs:
  - title: "OWASP Top 10:2025"
    url: https://top10.owasp.org/2025/
  - title: "OWASP API Security Top 10:2023"
    url: https://api-security.owasp.org/editions/2023/en/0x11-t10/
  - title: "OWASP Authorization Cheat Sheet"
    url: https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html
runner:
  race: true
taskKind: fix
---

Классы уязвимостей в Go те же, что в ASP.NET, — меняется то, кто их закрывает. В ASP.NET многое делает конвейер: `[Authorize]` и политики, антифорджери-токены, автоэкранирование Razor, лимит тела в Kestrel. В Go конвейера нет: `net/http` даёт `Handler` и middleware, а `CrossOriginProtection`, `html/template`, `MaxBytesReader` и проверки прав вы подключаете и пишете сами. Поэтому модуль начинается не с кода, а с вопроса «от чего защищаемся».

**Модель угроз** — три вопроса до первой строчки защиты. *Что защищаем* (активы): данные пользователей, деньги, секреты, доступность сервиса. *Где граница доверия*: всё, что пришло из-за неё, — ввод атакующего. Для веб-сервиса это тело, заголовки, URL и cookie запроса, но также ответы чужих API, файлы, загруженные пользователями, и строки из базы, которые туда когда-то записал пользователь. *Что может пойти не так* — здесь помогает STRIDE, список из шести классов угроз:

| Угроза | Что нарушено | Защита — урок модуля |
| --- | --- | --- |
| **S**poofing — выдать себя за другого | аутентификация | пароли и токены — 07, CSRF — 06 |
| **T**ampering — изменить данные | целостность | инъекции — 03–05, HMAC и AEAD — 08 |
| **R**epudiation — «я этого не делал» | неотказуемость | журнал без секретов — 11 |
| **I**nformation disclosure — утечка | конфиденциальность | IDOR — этот урок, ошибки и логи — 11, TLS — 08 |
| **D**enial of service — отказ | доступность | лимиты и таймауты — 02, 10 |
| **E**levation of privilege — чужие права | авторизация | проверки доступа — этот урок, SSRF — 09 |

**OWASP Top 10** — рейтинг рисков веб-приложений по данным реальных проверок; актуальная редакция — 2025. Для API есть отдельный список, API Security Top 10:2023. Это не чек-лист «закрыл десять пунктов — безопасен», а карта частых ошибок — по ней построен модуль:

| OWASP Top 10:2025 | Уроки |
| --- | --- |
| A01 Broken Access Control (в 2025 сюда вошёл SSRF) | 01, 04 (path traversal), 06, 09 |
| A02 Security Misconfiguration | 06, 08 (TLS), 10 |
| A03 Software Supply Chain Failures | 12 |
| A04 Cryptographic Failures | 07, 08 |
| A05 Injection (включая XSS) | 03, 04, 05 |
| A06 Insecure Design | 01, 13 |
| A07 Authentication Failures | 07 |
| A08 Software or Data Integrity Failures | 08, 12 |
| A09 Security Logging and Alerting Failures | 11 |
| A10 Mishandling of Exceptional Conditions | 02, 11 |

| OWASP API Security Top 10:2023 | Уроки |
| --- | --- |
| API1 Broken Object Level Authorization (IDOR) | 01 |
| API2 Broken Authentication | 07 |
| API3 Broken Object Property Level Authorization | 01 (mass assignment), 02 |
| API4 Unrestricted Resource Consumption | 02, 10 |
| API5 Broken Function Level Authorization | 01 |
| API6 Unrestricted Access to Sensitive Business Flows | 10 (лимиты на операцию) |
| API7 Server Side Request Forgery | 09 |
| API8 Security Misconfiguration | 06, 10 |
| API9 Improper Inventory Management | 13 |
| API10 Unsafe Consumption of APIs | 02, 09 |

Первое место в обоих списках — контроль доступа, и главная его ошибка — путать аутентификацию с авторизацией. Middleware узнаёт пользователя по токену, но о ресурсах ничего не знает: какой документ запрошен и чей он, становится известно только в хендлере, после загрузки из хранилища. Значит, и проверка «это объект текущего пользователя?» живёт там же — в каждом хендлере, который принимает `id` от клиента. Пропустили её в одном `DELETE` — получили IDOR (Insecure Direct Object Reference): атакующий просто перебирает `id`. Правило для всех проверок модуля: **запрещено всё, что явно не разрешено**, а право проверяется на сервере по данным сервера, а не по тому, что прислал клиент.

## Как это в C#

- `[Authorize]` и `.RequireAuthorization()` проверяют только «пользователь вошёл» или его роль — это аналог middleware аутентификации. Проверка объекта и в ASP.NET пишется руками: ресурсная авторизация через `IAuthorizationService.AuthorizeAsync(User, resource, requirement)` в action. В Go то же самое — функция вроде `ownDoc(user, id)`, без регистрации обработчиков в DI.
- Политика `[Authorize(Roles = "admin")]` на контроллере → проверка `user.Admin` в хендлере или отдельная middleware на группу маршрутов. В `ServeMux` нет групп с общими фильтрами: оборачивайте подмаршрутизатор (`mux.Handle("/admin/", requireAdmin(adminMux))`) или проверяйте в каждом хендлере.
- Overposting в ASP.NET (биндинг в сущность EF с `IsAdmin`) — то же, что декодирование тела в модель в Go. Лечится одинаково: отдельный DTO запроса только с разрешёнными полями.
- `Forbid()` и `NotFound()` выбираете сами в обоих стеках; для чужого объекта безопаснее `404`.

## Ловушки

### Идентичность из запроса

```go
// УЯЗВИМО: владельца называет клиент — подставит любое имя.
owner := r.URL.Query().Get("user") // или r.Header.Get("X-User-ID")
docs := store.ListByOwner(owner)
```

Всё, что пришло в запросе, кроме проверенного токена или сессии, — ввод атакующего. Пользователь берётся только из контекста, куда его положила middleware аутентификации: `docs := store.ListByOwner(userFrom(r).Name)`.

### Проверка есть в `GET`, но не в `DELETE`

```go
// УЯЗВИМО: проверку владельца написали в GET, а здесь забыли.
db.ExecContext(ctx, `DELETE FROM docs WHERE id = ?`, id)
```

Проверки, размазанные по хендлерам, забываются в одном из них. Надёжнее сделать владельца частью самого запроса к хранилищу — `DELETE FROM docs WHERE id = ? AND owner = ?` и `RowsAffected() == 0` → `404` — или держать единственную функцию доступа `ownDoc(user, id)`, которую вызывают все хендлеры.

### Декодирование прямо в модель

```go
type User struct {
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
}
// УЯЗВИМО: клиент пришлёт {"name":"bob","admin":true}.
json.NewDecoder(r.Body).Decode(&user)
```

Mass assignment (API3:2023): декодер заполнит любое экспортированное поле, ключи сравниваются даже без учёта регистра. Для каждого запроса — свой DTO только с теми полями, которые клиенту разрешено менять, и явное копирование в модель: `user.Name = in.Name`.

## Задание

В `task.go` — API документов. Код работает и проходит функциональные тесты, но **уязвим**: в нём несколько ошибок контроля доступа. Найдите и исправьте их, не меняя поведения для законных запросов.

```go
type Doc struct {
	ID    int64  `json:"id"`
	Owner string `json:"owner"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type User struct {
	Name  string
	Admin bool
}

func NewAPI(tokens map[string]User) http.Handler
```

`tokens` отображает токен на пользователя. Хранилище — в памяти, своё у каждого вызова `NewAPI`; запросы обрабатываются конкурентно (тесты идут с `-race`). Требования:

- Каждый запрос должен нести заголовок `Authorization: Bearer <токен>` с известным токеном, иначе — `401`.
- `POST /docs` — создать документ из JSON-тела (поля `title` и `body`). Если `json.Decoder.Decode` вернул ошибку — `400`. Иначе — `201` и созданный документ. `id` выдаёт сервер — с 1 по порядку, не переиспользуются; владелец нового документа — текущий пользователь.
- `GET /docs` — `200` и JSON-массив документов текущего пользователя по возрастанию `id` (`[]`, если их нет).
- `GET /docs/{id}` — `200` и документ; `DELETE /docs/{id}` — удалить, `204` без тела. Пользователю доступны только его документы. Если `{id}` — не положительное целое, документа нет или он принадлежит другому пользователю — `404`; ответ на чужой документ не должен отличаться от ответа на несуществующий.
- `GET /admin/docs` — `200` и все документы по возрастанию `id`, только для пользователей с `Admin: true`; остальным — `403`. Других особых прав у администратора нет: чужие документы через `/docs/{id}` ему тоже недоступны.
- Успешные ответы с телом — `Content-Type: application/json`; ошибки — `http.Error` с любым текстом.
