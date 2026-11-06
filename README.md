# Go для C#-разработчика

Интерактивный учебник по Go для разработчиков с опытом C#/.NET: уроки с аннотированными примерами, блоком «Как это в C#», ловушками, квизами и задачами с автотестами.

## Структура

| Путь | Что там |
| --- | --- |
| `content/` | Уроки: папка на урок с настоящими `.go`-файлами. Формат — [content/README.md](content/README.md) |
| `tools/lessonlint/` | Проверка структуры и прогон каждого урока |
| `site/` | Сайт на Astro, собирается из `content/` |
| `runner/` | Сервис исполнения кода (этап 2) |
| `viz/` | Движок визуализаций (этап 4) |

## Команды

```sh
# проверить весь контент: пример, эталон, шаблон задачи, квизы
go run ./tools/lessonlint content

# только один урок; -update перезаписывает example/output.txt
go run ./tools/lessonlint -run 05-concurrency/07-select -update content

# сайт
cd site && npm ci && npm run dev     # http://localhost:4321
cd site && npm run build             # статика в site/dist
```

Нужны Go 1.25+ и Node 24.
