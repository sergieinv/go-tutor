# Образ сервиса «Заказы». Собирается из каталога labs/orders:
#   docker build -t go-tutor/orders labs/orders
# или через docker compose --profile labs build orders.

# Сборка. Модуль объявляет go 1.25.0; тулчейн новее собирает его с той же
# семантикой языка (директива go в go.mod) — образ тот же, что у runner.
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
# Сначала только go.mod/go.sum: слой с модулями кешируется, пока не
# меняются зависимости.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0 — статический бинарник: драйвер SQLite на чистом Go,
# libc в финальном образе не нужна. -trimpath убирает пути сборочной машины.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/orders ./cmd/orders

# Финальный образ — scratch: только бинарник и корневые сертификаты (для
# проверки сертификата базы при sslmode=verify-full; sslmode=require шифрует,
# но сертификат не проверяет). Ни оболочки, ни пакетного менеджера —
# нечего атаковать и нечего обновлять. Миграции встроены в бинарник (embed).
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/orders /orders
# Не root: числовой uid, в scratch нет /etc/passwd.
USER 65532:65532
ENV ORDERS_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/orders"]
