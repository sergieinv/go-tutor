## Подсказка 1

Пройдите по чек-листу из урока сверху вниз и для каждого хендлера спросите: что в нём приходит от клиента (путь, параметры запроса, тело, содержимое уже сохранённых заметок) и куда это попадает — в SQL, в HTML, в путь к файлу, в ответ. Уязвимостей шесть, и ни одна не требует менять сигнатуры: права на объект, SQL, HTML, файлы, ошибки, размер запроса.

## Подсказка 2

Подозрительные места: `ORDER BY ` + значение из запроса; выборка заметки `WHERE id = ?` — без пользователя, а её используют два маршрута; `template.HTML(...)` от текста заметки; `filepath.Join(s.files, name)`; `err.Error()` в функции `internal`; `json.NewDecoder(r.Body)` без ограничения тела. Проверьте каждое мысленной атакой: чужой `id`, `sort=(CASE WHEN … END)`, тело `<script>`, `name=../secret`, закрытая база, мегабайтный JSON.

## Подсказка 3

- права: `WHERE id = ? AND owner = ?` в функции `note` — оба маршрута получат проверку сразу;
- сортировка: `map[string]string{"": "id", "id": "id", "title": "title"}`, нет ключа — `400`;
- HTML: `template.HTML(strings.ReplaceAll(template.HTMLEscapeString(n.Body), "\n", "<br>"))` — сначала экранировать, потом добавить свою разметку;
- файлы: `root, err := os.OpenRoot(s.files)`, затем `root.Stat(name)` (ошибка или `!info.Mode().IsRegular()` — `404`) и `root.ReadFile(name)`; `os.Root` не выпускает ни через `..`, ни через ссылки;
- ошибки: в `internal` — `log.Printf` с подробностями и `http.Error(w, "internal error", 500)`;
- тело: `r.Body = http.MaxBytesReader(w, r.Body, 64<<10)` и `errors.As(err, &maxErr)` → `413`.
