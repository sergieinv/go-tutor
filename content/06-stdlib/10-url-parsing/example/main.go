// `net/url` разбирает URL на части и собирает обратно,
// экранируя то, что нужно. `url.Parse` строг к синтаксису,
// но не проверяет, что схема или хост осмысленные.

package main

import (
	"fmt"
	"net/url"
)

func main() {
	u, err := url.Parse("https://anna:secret@Shop.example.com:8443/каталог/a%20b?q=go+lang&tag=x&tag=y#reviews")
	if err != nil {
		fmt.Println(err)
		return
	}

	// `Host` — хост с портом, `Hostname` и `Port` — по
	// отдельности. Регистр хоста не меняется.
	fmt.Println(u.Scheme, u.Host, u.Hostname(), u.Port())

	// `Path` — уже раскодированный путь, `EscapedPath` —
	// в виде для передачи по сети. `RawQuery` — запрос
	// как есть, `Fragment` — после `#`.
	fmt.Println(u.Path, "|", u.EscapedPath())
	fmt.Println(u.RawQuery, "|", u.Fragment, "|", u.User.Username())

	// `Query` разбирает запрос в `url.Values` —
	// `map[string][]string`: ключ может повторяться.
	// `Get` возвращает первое значение или "".
	q := u.Query()
	fmt.Println(q.Get("q"), q["tag"], q.Get("page") == "", q.Has("tag"))

	// `Values` можно и собирать: `Encode` сортирует ключи
	// и экранирует значения. Изменения `q` не попадают в `u`
	// сами — запишите `u.RawQuery = q.Encode()`.
	q.Set("page", "2")
	q.Del("tag")
	u.RawQuery = q.Encode()
	u.Fragment = ""
	u.User = nil
	fmt.Println(u)

	// Экранирование: `QueryEscape` — для значения
	// параметра (пробел → `+`, `/` → `%2F`), `PathEscape` —
	// для одного сегмента пути (пробел → `%20`).
	fmt.Println(url.QueryEscape("a b/c&d"), url.PathEscape("a b/c&d"))

	// Надёжнее собирать URL из структуры: `String`
	// экранирует путь сам.
	b := url.URL{Scheme: "https", Host: "api.example.com",
		Path: "/users/Анна К", RawQuery: url.Values{"v": {"1 2"}}.Encode()}
	fmt.Println(b.String())

	// `JoinPath` добавляет сегменты пути, экранирует их
	// и расставляет `/` без двойных слешей.
	j, _ := url.JoinPath("https://api.example.com/v1/", "users", "a b")
	fmt.Println(j)

	// `ResolveReference` — относительная ссылка от базы,
	// как браузер разрешает `href` на странице.
	base, _ := url.Parse("https://example.com/docs/guide/intro")
	for _, ref := range []string{"setup", "../api", "/about", "?page=2"} {
		r, _ := url.Parse(ref)
		fmt.Println(base.ResolveReference(r))
	}
}
