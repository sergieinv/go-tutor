# Образ сервиса «Уведомления». Контекст сборки — каталог labs/: модуль
# ссылается на labs/api и labs/platform через replace в go.mod.
#   docker build -f labs/notifications/Dockerfile -t go-tutor/notifications labs
# или через docker compose --profile labs build notifications.

# Сборка. Модуль объявляет go 1.25.0; тулчейн новее собирает его с той же
# семантикой языка (директива go в go.mod) — образ тот же, что у runner.
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
# Сначала только go.mod/go.sum всех трёх модулей: слой со скачанными
# зависимостями кешируется, пока они не меняются.
COPY api/go.mod api/go.sum api/
COPY platform/go.mod platform/go.sum platform/
COPY notifications/go.mod notifications/go.sum notifications/
RUN cd notifications && go mod download
COPY api/ api/
COPY platform/ platform/
COPY notifications/ notifications/
# CGO_ENABLED=0 — статический бинарник: драйвер SQLite (modernc.org/sqlite)
# на чистом Go, libc в финальном образе не нужна. -trimpath убирает пути
# сборочной машины, -s -w — таблицы символов и отладочную информацию.
RUN cd notifications && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/notifications ./cmd/notifications

# Финальный образ — scratch: только бинарник и корневые сертификаты (для
# TLS до брокера и коллектора трасс в продакшне). Ни оболочки, ни пакетного
# менеджера — нечего атаковать и нечего обновлять. Миграции встроены в
# бинарник (embed).
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/notifications /notifications
# Не root: числовой uid, в scratch нет /etc/passwd. Каталог базы (/data)
# должен быть доступен этому uid на запись — в compose это tmpfs с uid=65532.
USER 65532:65532
# Только служебный HTTP: /healthz, /readyz, /metrics. Входящего API у
# сервиса нет — работа приходит из Kafka.
EXPOSE 8081
ENTRYPOINT ["/notifications"]
