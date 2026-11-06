# Образ runner: тулчейн Go, прогретый кеш сборки и контент уроков.
# Собирается из корня репозитория: docker build -f runner/Dockerfile .
FROM golang:1.27.2-bookworm AS build
WORKDIR /src/runner
COPY runner/go.mod runner/go.sum ./
RUN go mod download
COPY runner/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/runner .

FROM golang:1.27.2-bookworm
# Сети у песочницы нет: модули — только из кеша образа.
ENV GOTOOLCHAIN=local \
    GOFLAGS=-mod=mod \
    GOPROXY=off \
    GOCACHE=/var/cache/go-build \
    RUNNER_MODE=local \
    RUNNER_CONTENT=/content \
    RUNNER_ADDR=:8081
# Прогрев: стандартная библиотека в обычной и race-сборке и test2json,
# чтобы первый запуск урока укладывался в 1–2 секунды.
RUN go build std && go build -race std && go tool test2json < /dev/null
COPY --from=build /out/runner /usr/local/bin/runner
COPY content /content
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s CMD curl -fsS http://localhost:8081/api/health || exit 1
CMD ["runner"]
