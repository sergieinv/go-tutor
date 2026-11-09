#!/usr/bin/env bash
# Образы учебника с тегом версии курса (файл VERSION в корне).
#
#   scripts/images.sh build   собрать все собственные образы: теги <версия> и dev,
#                             метки OCI (версия, коммит, время сборки, источник)
#   scripts/images.sh list    имена образов
#   scripts/images.sh push    опубликовать тег <версия> в реестр GOTUTOR_REGISTRY
#
# Имена — ${GOTUTOR_REGISTRY:-go-tutor}/<имя>:<версия>, как в docker-compose.yml.
# push работает только с явно заданным реестром и в реестр не логинится:
# docker login — заранее, своими средствами.
#   GOTUTOR_REGISTRY=ghcr.io/<владелец>/go-tutor scripts/images.sh push
# Нужен только docker: плагин «docker compose» или отдельный docker-compose.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

# Собственные образы: сервисы docker-compose.yml с секцией build.
services=(site runner orders warehouse notifications loadgen gameserver gamebots)

title() {
  case $1 in
    site) echo "go-tutor: сайт учебника (Astro + nginx)" ;;
    runner) echo "go-tutor: runner — песочница для кода уроков" ;;
    orders) echo "go-tutor: сервис «Заказы» (labs)" ;;
    warehouse) echo "go-tutor: сервис «Склад» (labs)" ;;
    notifications) echo "go-tutor: сервис «Уведомления» (labs)" ;;
    loadgen) echo "go-tutor: генератор нагрузки (labs)" ;;
    gameserver) echo "go-tutor: игровой сервер модуля 15 (labs)" ;;
    gamebots) echo "go-tutor: нагрузочные боты игрового сервера (labs)" ;;
  esac
}

die() {
  echo "images.sh: $*" >&2
  exit 1
}

# Печатает команду и выполняет её.
run() {
  echo "+ $*" >&2
  "$@"
}

version=$(tr -d '[:space:]' < VERSION)
# Тег Docker: буквы, цифры, «_», «.», «-», не длиннее 128 символов.
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]] \
  || die "VERSION: «$version» — не версия вида 1.2.3"

default_registry=go-tutor
registry=${GOTUTOR_REGISTRY:-$default_registry}

# У пользователей обычно плагин «docker compose», на некоторых машинах —
# только отдельный docker-compose.
compose=()
if docker compose version >/dev/null 2>&1; then
  compose=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  compose=(docker-compose)
else
  die "нужен docker compose (плагин) или docker-compose"
fi

# Адрес репозитория для метки source: только https, без учётных данных.
source_url() {
  local url
  url=$(git remote get-url origin 2>/dev/null) || return 0
  case $url in
    git@*:*) url="${url#git@}" && url="https://${url/://}" ;;
    ssh://git@*) url="https://${url#ssh://git@}" ;;
    https://*@*) url="https://${url#https://*@}" ;;
  esac
  if [[ $url == https://* ]]; then
    echo "${url%.git}"
  fi
}

cmd_list() {
  local s
  for s in "${services[@]}"; do
    echo "$registry/$s:$version"
  done
}

cmd_build() {
  local revision created source s
  revision=$(git rev-parse HEAD 2>/dev/null || echo unknown)
  if [[ $revision != unknown && -n $(git status --porcelain 2>/dev/null) ]]; then
    echo "images.sh: в рабочей копии есть незакоммиченные изменения — метка revision получит суффикс -dirty" >&2
    revision="$revision-dirty"
  fi
  created=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  source=$(source_url)

  # Метки OCI — во временном compose-файле поверх docker-compose.yml: в
  # основном файле они были бы пустыми при обычном up --build.
  # labels — глобальная: trap EXIT срабатывает уже после выхода из функции.
  labels=$(mktemp "${TMPDIR:-/tmp}/go-tutor-labels.XXXXXX")
  trap 'rm -f "$labels"' EXIT
  {
    echo "services:"
    for s in "${services[@]}"; do
      echo "  $s:"
      echo "    build:"
      echo "      labels:"
      echo "        org.opencontainers.image.title: \"$(title "$s")\""
      echo "        org.opencontainers.image.version: \"$version\""
      echo "        org.opencontainers.image.revision: \"$revision\""
      echo "        org.opencontainers.image.created: \"$created\""
      if [[ -n $source ]]; then
        echo "        org.opencontainers.image.source: \"$source\""
      fi
    done
  } > "$labels"

  echo "Сборка образов версии $version (revision $revision) через ${compose[*]}" >&2
  GOTUTOR_VERSION=$version GOTUTOR_REGISTRY=$registry run "${compose[@]}" \
    -f docker-compose.yml -f "$labels" --profile labs --profile loadtest build "${services[@]}"

  # dev — тег, который docker-compose.yml берёт без переменных: обычный
  # docker compose up запустится из только что собранных образов.
  if [[ $version != dev ]]; then
    for s in "${services[@]}"; do
      run docker tag "$registry/$s:$version" "$registry/$s:dev"
    done
  fi
  echo "Готово:" >&2
  for s in "${services[@]}"; do
    echo "  $registry/$s:$version, $registry/$s:dev" >&2
  done
}

cmd_push() {
  [[ -n ${GOTUTOR_REGISTRY:-} && $GOTUTOR_REGISTRY != "$default_registry" ]] \
    || die "push: задайте реестр явно, например GOTUTOR_REGISTRY=ghcr.io/<владелец>/go-tutor"
  local s src dst
  for s in "${services[@]}"; do
    dst="$registry/$s:$version"
    # Образы, собранные без GOTUTOR_REGISTRY, получают имя реестра тегом.
    if ! docker image inspect "$dst" >/dev/null 2>&1; then
      src="$default_registry/$s:$version"
      docker image inspect "$src" >/dev/null 2>&1 \
        || die "push: нет образа $dst или $src — сначала scripts/images.sh build"
      run docker tag "$src" "$dst"
    fi
  done
  # Публикуется только тег версии: dev — локальный.
  for s in "${services[@]}"; do
    run docker push "$registry/$s:$version"
  done
}

case ${1:-} in
  build) cmd_build ;;
  list) cmd_list ;;
  push) cmd_push ;;
  *)
    echo "использование: scripts/images.sh build | list | push" >&2
    exit 2
    ;;
esac
