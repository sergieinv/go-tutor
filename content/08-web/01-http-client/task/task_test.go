package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type user struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestGetJSONOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":7,"name":"Анна"}`)
	}))
	defer srv.Close()

	var u user
	err := GetJSON(context.Background(), &http.Client{Timeout: 5 * time.Second}, srv.URL, 1024, &u)
	if err != nil || u != (user{7, "Анна"}) {
		t.Fatalf("GetJSON = %v, u = %+v; want nil, {7 Анна}", err, u)
	}
}

func TestGetJSONStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "нет такого", http.StatusNotFound)
	}))
	defer srv.Close()

	var u user
	err := GetJSON(context.Background(), srv.Client(), srv.URL, 1024, &u)
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusNotFound {
		t.Fatalf("GetJSON при 404 = %v; want *StatusError{404}", err)
	}
}

func TestGetJSONNotJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<h1>привет</h1>")
	}))
	defer srv.Close()

	var u user
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, 1024, &u); !errors.Is(err, ErrNotJSON) {
		t.Fatalf("GetJSON с text/html = %v; want ErrNotJSON", err)
	}
}
