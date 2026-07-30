package webui

import (
	_ "embed"
	"net/http"
)

//go:embed dashboard.html
var dashboard []byte

//go:embed settings.html
var settings []byte

//go:embed setup.html
var setup []byte

func Dashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(dashboard)
}

func Settings(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(settings)
}

func Setup(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(setup)
}
