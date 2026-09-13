package qbit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIKeyAuthenticatesReadAndActionRequests(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer qbt_test-key" {
			t.Errorf("Authorization = %q", got)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/stop":
			if err := r.ParseForm(); err != nil || r.Form.Get("hashes") != "abc" {
				t.Errorf("unexpected stop form: %v", r.Form)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "ignored", "ignored", "qbt_test-key", "api_key")
	if err := client.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Torrents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Pause(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.Contains(path, "/auth/") {
			t.Fatalf("API key mode called auth endpoint: %s", path)
		}
	}
}

func TestAutoPrefersAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			t.Fatal("auto mode attempted password login while API key was configured")
		}
		if r.Header.Get("Authorization") != "Bearer qbt_auto-key" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := New(server.URL, "user", "password", "qbt_auto-key", "auto")
	if _, err := client.Torrents(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordLoginRemainsSupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			if err := r.ParseForm(); err != nil || r.Form.Get("username") != "user" || r.Form.Get("password") != "secret" {
				t.Errorf("unexpected login form: %v", r.Form)
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			cookie, err := r.Cookie("SID")
			if err != nil || cookie.Value != "session" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "user", "secret", "", "password")
	if err := client.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Torrents(context.Background()); err != nil {
		t.Fatal(err)
	}
}
