# Образ генератора нагрузки (labs/loadgen). Контекст сборки — каталог
# labs/, как у сервисов. Зависимостей вне стандартной библиотеки нет.
#   docker compose --profile labs run --rm loadgen analyze
FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY loadgen/ loadgen/
RUN cd loadgen && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/loadgen ./cmd/loadgen

# scratch и не root — как у сервисов. Генератор только ходит по HTTP в
# «Заказы» внутри docker-сети; портов не слушает.
FROM scratch
COPY --from=build /out/loadgen /loadgen
USER 65532:65532
ENTRYPOINT ["/loadgen"]
CMD ["analyze"]
