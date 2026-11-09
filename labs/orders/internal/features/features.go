// Package features — переключатели функций «Заказов», которые можно менять
// без перезапуска: кеш каталога и чтение истории с реплики (модуль 14).
//
// Зачем: нагрузочный разбор (labs/loadgen analyze) сравнивает прогоны «без
// кеша», «с кешем», «с репликой» на одном и том же запущенном сервисе.
// Перезапустить контейнер изнутри другого контейнера нельзя (и не нужно
// давать генератору нагрузки доступ к Docker), а пересборка ради флага
// растягивает разбор и сбрасывает прогрев. Поэтому — служебный эндпоинт
// /admin/features на ops-порту:
//
//	GET  /admin/features                 → {"catalog_cache": true, "replica_reads": true}
//	POST /admin/features {"catalog_cache": false}
//
// Эндпоинт выключен по умолчанию (ORDERS_ADMIN_FEATURES) и живёт только на
// служебном порту: в compose он опубликован на 127.0.0.1 и виден в
// docker-сети default, но не в сети песочницы. Аутентификации нет — как и у
// остальных служебных эндпоинтов стенда; в проде такие переключатели живут
// в системе feature flags (LaunchDarkly, Unleash, flagd) с аудитом.
//
// Включить функцию, которой нет (кеш без ORDERS_VALKEY_ADDR, реплика без
// ORDERS_REPLICA_DSN), нельзя — 409.
package features

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
)

// Flags — текущие значения переключателей. Методы безопасны для
// конкурентного вызова: их читает каждый запрос.
type Flags struct {
	cacheAvailable, replicaAvailable bool
	cache, replica                   atomic.Bool
}

// New создаёт переключатели. Доступная функция включена сразу.
func New(cacheAvailable, replicaAvailable bool) *Flags {
	f := &Flags{cacheAvailable: cacheAvailable, replicaAvailable: replicaAvailable}
	f.cache.Store(cacheAvailable)
	f.replica.Store(replicaAvailable)
	return f
}

// CatalogCache — включён ли кеш каталога.
func (f *Flags) CatalogCache() bool { return f.cache.Load() }

// ReplicaReads — читать ли историю с реплики.
func (f *Flags) ReplicaReads() bool { return f.replica.Load() }

// State — JSON эндпоинта.
type State struct {
	CatalogCache *bool `json:"catalog_cache,omitempty"`
	ReplicaReads *bool `json:"replica_reads,omitempty"`
}

func (f *Flags) state() State {
	c, r := f.CatalogCache(), f.ReplicaReads()
	return State{CatalogCache: &c, ReplicaReads: &r}
}

var errUnavailable = errors.New("feature is not configured")

// Set меняет переданные (не nil) переключатели.
func (f *Flags) Set(s State) error {
	if (s.CatalogCache != nil && *s.CatalogCache && !f.cacheAvailable) ||
		(s.ReplicaReads != nil && *s.ReplicaReads && !f.replicaAvailable) {
		return errUnavailable
	}
	if s.CatalogCache != nil {
		f.cache.Store(*s.CatalogCache)
	}
	if s.ReplicaReads != nil {
		f.replica.Store(*s.ReplicaReads)
	}
	return nil
}

// Handler — GET и POST /admin/features. Порт открыт только на 127.0.0.1,
// но браузер ученика дотянется и туда: POST с чужой страницы (no-cors,
// text/plain) отвергает CrossOriginProtection по Sec-Fetch-Site и Origin.
func (f *Flags) Handler(log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/features", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, f.state())
	})
	mux.HandleFunc("POST /admin/features", func(w http.ResponseWriter, r *http.Request) {
		var s State
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "want JSON like {\"catalog_cache\": false}"})
			return
		}
		if err := f.Set(s); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		st := f.state()
		log.WarnContext(r.Context(), "features changed", "catalog_cache", *st.CatalogCache, "replica_reads", *st.ReplicaReads)
		writeJSON(w, http.StatusOK, st)
	})
	return http.NewCrossOriginProtection().Handler(mux)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
