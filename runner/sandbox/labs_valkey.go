package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Valkey песочницы — отдельный экземпляр (valkey-sandbox), общий на все
// запуски. Базы на запуск у Valkey нет, поэтому runner создаёт
// ACL-пользователя run_…, которому доступны только ключи и каналы с
// префиксом «run_…:» и не доступны опасные команды (FLUSHALL, CONFIG, KEYS,
// ACL, DEBUG…). После запуска runner удаляет ключи с префиксом и
// пользователя — его соединения Valkey закрывает сам.

// valkeyACL — права пользователя запуска.
var valkeyACL = []string{"+@all", "-@dangerous"}

// createValkey заводит пользователя запуска и выдаёт переменные окружения.
func createValkey(ctx context.Context, admin, id, password string) ([]string, error) {
	c, addr, err := valkeyAdmin(admin)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	// id и password — hex из crypto/rand: в аргументах ACL SETUSER нет пробелов.
	rules := append([]string{"reset", "on", ">" + password, "~" + id + ":*", "&" + id + ":*"}, valkeyACL...)
	cmd := c.B().AclSetuser().Username(id).Rule(rules...).Build()
	if err := c.Do(ctx, cmd).Error(); err != nil {
		return nil, err
	}
	return []string{
		"VALKEY_ADDR=" + addr,
		"VALKEY_USERNAME=" + id,
		"VALKEY_PASSWORD=" + password,
		"VALKEY_PREFIX=" + id + ":",
	}, nil
}

// dropValkey удаляет пользователей запусков, принятых owned, и их ключи.
func dropValkey(ctx context.Context, admin string, owned func(id string) bool) error {
	c, _, err := valkeyAdmin(admin)
	if err != nil {
		return err
	}
	defer c.Close()
	users, err := c.Do(ctx, c.B().AclUsers().Build()).AsStrSlice()
	if err != nil {
		return err
	}
	for _, id := range users {
		if !validLabID(id) || !owned(id) {
			continue
		}
		// Сначала пользователь: ACL DELUSER рвёт его соединения, и
		// программа, которую не успели убить, не допишет ключей после уборки.
		c.Do(ctx, c.B().AclDeluser().Username(id).Build())
		var cursor uint64
		for {
			e, err := c.Do(ctx, c.B().Scan().Cursor(cursor).Match(id+":*").Count(500).Build()).AsScanEntry()
			if err != nil {
				return err
			}
			if len(e.Elements) > 0 {
				c.Do(ctx, c.B().Unlink().Key(e.Elements...).Build())
			}
			if cursor = e.Cursor; cursor == 0 {
				break
			}
		}
	}
	return nil
}

// valkeyAdmin подключается по адресу вида valkey://default:пароль@хост:порт.
func valkeyAdmin(admin string) (valkey.Client, string, error) {
	u, err := url.Parse(admin)
	if err != nil || u.Host == "" {
		return nil, "", fmt.Errorf("адрес Valkey: нужен valkey://user:password@host:port")
	}
	password, _ := u.User.Password()
	c, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{u.Host},
		Username:     u.User.Username(),
		Password:     password,
		DisableCache: true,
		// Профиль labs не запущен — имя не резолвится; ждать не нужно.
		Dialer: net.Dialer{Timeout: 2 * time.Second},
		// Без повторов: уборка и создание и так ограничены labTimeout.
		DisableRetry: true,
	})
	if err != nil {
		return nil, "", err
	}
	return c, strings.TrimPrefix(u.Host, "//"), nil
}
