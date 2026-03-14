package app

import "net/http"

// App wires together the store and HTTP mux for the test API fixture.
type App struct {
	store *Store
	mux   *http.ServeMux
}

// New creates a new App with a seeded in-memory store.
func New() *App {
	a := &App{
		store: NewStore(),
		mux:   http.NewServeMux(),
	}
	registerRoutes(a)
	return a
}

// Handler returns the HTTP handler for the app.
func (a *App) Handler() http.Handler { return a.mux }
