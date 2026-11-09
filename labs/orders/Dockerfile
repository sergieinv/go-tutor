# Образ сервиса «Заказы». Контекст сборки — каталог labs/: модуль ссылается
# на labs/api, labs/platform (и в тестах на labs/warehouse) через replace.
#   docker build -f labs/orders/Dockerfile -t go-tutor/orders labs
# или через docker compose --profile labs build orders.

# Сборка. Модуль объявляет go 1.25.0; тулчейн новее собирает его с той же
# семантикой языка (директива go в go.mod) — образ тот же, что у runner.
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
# Сначала только go.mod/go.sum: слой со скачанными зависимостями
# кешируется, пока они не меняются. go.mod «Склада» нужен, потому что он
# есть в графе модулей (тестовая зависимость warehousetest); его код в
# бинарник не попадает.
COPY api/go.mod api/go.sum api/
COPY platform/go.mod platform/go.sum platform/
COPY warehouse/go.mod warehouse/go.sum warehouse/
COPY orders/go.mod orders/go.sum orders/
RUN cd orders && go mod download
COPY api/ api/
COPY platform/ platform/
COPY orders/ orders/
# CGO_ENABLED=0 — статический бинарник: драйверы PostgreSQL и SQLite на
# чистом Go, libc в финальном образе не нужна. -trimpath убирает пути
# сборочной машины, -s -w — таблицы символов и отладочную информацию.
RUN cd orders && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/orders ./cmd/orders

# Финальный образ — scratch: только бинарник и корневые сертификаты (для
# проверки сертификата базы при sslmode=verify-full). Ни оболочки, ни
# пакетного менеджера — нечего атаковать и нечего обновлять. Миграции
# встроены в бинарник (embed).
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/orders /orders
# Не root: числовой uid, в scratch нет /etc/passwd.
USER 65532:65532
# 8080 — REST (grpc-gateway), 9090 — gRPC, 8081 — служебный HTTP
# (/healthz, /readyz, /metrics; наружу не публикуется).
EXPOSE 8080 9090 8081
ENTRYPOINT ["/orders"]
