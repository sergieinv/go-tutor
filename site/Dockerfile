# Образ сайта: статическая сборка Astro за nginx, /api/* проксируется в runner.
# Собирается из корня репозитория: docker build -f site/Dockerfile .
FROM node:24-bookworm-slim AS build
# lock-файл записан через корпоративный Artifactory, а в контейнере нет его
# сертификата. npm ci подменяет префикс LOCK_REGISTRY в resolved-адресах на
# NPM_REGISTRY; integrity-хеши из lock-файла всё равно проверяются.
ARG NPM_REGISTRY=https://registry.npmjs.org/
ARG LOCK_REGISTRY=https://artifactory.s.o3.ru/artifactory/api/npm/npm-all-virtual
WORKDIR /app/site
COPY site/package.json site/package-lock.json ./
RUN npm ci --registry="$NPM_REGISTRY" --replace-registry-host="$LOCK_REGISTRY"
COPY site/ ./
COPY content /app/content
# Движок визуализаций: сайт импортирует его исходники напрямую.
COPY viz /app/viz
RUN npx astro telemetry disable && npm run build

FROM nginx:1.29-alpine
COPY site/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/site/dist /usr/share/nginx/html
EXPOSE 8080
