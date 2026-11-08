// go.mod по умолчанию для программ песочницы (рядом — deps.sum).
// Единственные сторонние модули, доступные урокам: у песочницы нет сети,
// образ runner скачивает их в кеш модулей при сборке. Добавить модуль:
// скопируйте deps.mod/deps.sum во временный каталог как go.mod/go.sum,
// выполните там GOWORK=off go get example.com/mod@vX.Y.Z (версия не моложе
// двух недель, её директива go не выше go здесь — иначе курс перейдёт
// на новую версию языка) и верните файлы обратно.
module lesson

go 1.25.0

require (
	golang.org/x/crypto v0.55.0
	golang.org/x/sync v0.22.0
	modernc.org/sqlite v1.59.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
