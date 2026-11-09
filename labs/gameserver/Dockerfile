# Образы игрового сервера (labs/gameserver, модуль 15). Контекст сборки —
# каталог labs/, как у остальных сервисов. Две финальные цели:
#   gameserver — сервер (docker-compose --profile labs up -d --build gameserver)
#   bots       — нагрузочные боты (docker-compose --profile labs run --rm gamebots)
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
# Сначала go.mod/go.sum — слой с зависимостями кешируется.
COPY gameserver/go.mod gameserver/go.sum gameserver/
RUN cd gameserver && go mod download
COPY gameserver/ gameserver/
# Статические бинарники: CGO не нужен, -trimpath убирает пути сборочной
# машины, -s -w — таблицы символов.
RUN cd gameserver && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gameserver ./cmd/gameserver \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bots ./cmd/bots
# Каталог сохранений с владельцем 65532: именованный том при первом
# монтировании наследует владельца из образа.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

# Сервер: scratch, не root. Ресурсов Mojang в образе нет — только бинарник.
FROM scratch AS gameserver
COPY --from=build /out/gameserver /gameserver
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
# 25565 — Minecraft, 25580 — WebSocket-шлюз, 25590 — служебный HTTP
# (/metrics, /healthz, /readyz, /debug/pprof).
EXPOSE 25565 25580 25590
ENTRYPOINT ["/gameserver"]

# Боты: тот же scratch, портов не слушают.
FROM scratch AS bots
COPY --from=build /out/bots /bots
USER 65532:65532
ENTRYPOINT ["/bots"]
