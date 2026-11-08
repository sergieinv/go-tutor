# Образ runner: тулчейн Go, прогретый кеш сборки и контент уроков.
# Собирается из корня репозитория: docker build -f runner/Dockerfile .
FROM golang:1.27.2-bookworm AS build
WORKDIR /src/runner
COPY runner/go.mod runner/go.sum ./
RUN go mod download
COPY runner/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/runner .
# govulncheck для урока о зависимостях — инструмент runner, а не модуль урока,
# поэтому его версия не ограничена версией языка курса.
RUN CGO_ENABLED=0 GOBIN=/out go install -trimpath golang.org/x/vuln/cmd/govulncheck@v1.9.0

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
# Сторонние модули уроков (sandbox/deps.mod): скачиваем, пока сеть есть, и
# прогреваем их сборку. GOFLAGS=-mod=mod не даёт go build тронуть go.mod.
COPY runner/sandbox/deps.mod /tmp/deps/go.mod
COPY runner/sandbox/deps.sum /tmp/deps/go.sum
RUN cd /tmp/deps && GOPROXY=https://proxy.golang.org GOFLAGS= go mod download all \
    && GOFLAGS= go build golang.org/x/sync/... golang.org/x/crypto/bcrypt golang.org/x/crypto/argon2 modernc.org/sqlite && rm -rf /tmp/deps
# Снимок базы уязвимостей Go для govulncheck: ADD по URL сверяет архив
# при каждой сборке, так что пересборка образа обновляет снимок.
# Дату снимка (index/db.json) runner отдаёт вместе с результатом.
ADD https://vuln.go.dev/vulndb.zip /tmp/vulndb.zip
RUN python3 -m zipfile -e /tmp/vulndb.zip /opt/vulndb && rm /tmp/vulndb.zip \
    && chmod -R a+rX /opt/vulndb && cat /opt/vulndb/index/db.json
ENV RUNNER_VULNDB=file:///opt/vulndb
COPY --from=build /out/runner /out/govulncheck /usr/local/bin/
COPY content /content
# Примеры со своим go.mod (например, уязвимая версия модуля для govulncheck):
# всё, что перечислено в их go.sum, скачивается в кеш, пока у сборки есть сеть,
# — в том числе исправленная версия, на которую ученик обновит go.mod.
RUN for s in /content/*/*/example/go.sum; do \
      [ -f "$s" ] || continue; \
      d=$(mktemp -d) && cp "$(dirname "$s")/go.mod" "$s" "$d" \
      && (cd "$d" && awk '{ sub(/\/go\.mod$/, "", $2); print $1 "@" $2 }' go.sum | sort -u \
          | GOPROXY=https://proxy.golang.org GOFLAGS=-mod=mod xargs go mod download) \
      && rm -rf "$d" || exit 1; \
    done
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s CMD curl -fsS http://localhost:8081/api/health || exit 1
CMD ["runner"]
