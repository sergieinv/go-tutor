# Образ сервиса «Склад». Контекст сборки — каталог labs/: модуль ссылается
# на labs/api и labs/platform через replace в go.mod.
#   docker build -f labs/warehouse/Dockerfile -t go-tutor/warehouse labs
# или через docker compose --profile labs build warehouse.

# Сборка. Модуль объявляет go 1.25.0; тулчейн новее собирает его с той же
# семантикой языка (директива go в go.mod) — образ тот же, что у runner.
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
# Сначала только go.mod/go.sum всех трёх модулей: слой со скачанными
# зависимостями кешируется, пока они не меняются, — правка кода не
# скачивает модули заново.
COPY api/go.mod api/go.sum api/
COPY platform/go.mod platform/go.sum platform/
COPY warehouse/go.mod warehouse/go.sum warehouse/
RUN cd warehouse && go mod download
COPY api/ api/
COPY platform/ platform/
COPY warehouse/ warehouse/
# CGO_ENABLED=0 — статический бинарник (драйвер MySQL на чистом Go), libc
# в финальном образе не нужна. -trimpath убирает пути сборочной машины,
# -s -w — таблицы символов и отладочную информацию.
RUN cd warehouse && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/warehouse ./cmd/warehouse

# Финальный образ — scratch: только бинарник и корневые сертификаты (для
# TLS до MySQL с проверкой сертификата, tls=true в DSN). Ни оболочки, ни
# пакетного менеджера — нечего атаковать и нечего обновлять. Миграции
# встроены в бинарник (embed).
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/warehouse /warehouse
# Не root: числовой uid, в scratch нет /etc/passwd.
USER 65532:65532
# 8080 — REST (grpc-gateway), 9090 — gRPC, 8081 — служебный HTTP
# (/healthz, /readyz, /metrics; наружу не публикуется).
EXPOSE 8080 9090 8081
ENTRYPOINT ["/warehouse"]
