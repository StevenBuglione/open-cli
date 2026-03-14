package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func registerRoutes(a *App) {
	mux := a.mux

	// Items collection
	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			a.handleListItems(w, r)
		case http.MethodPost:
			a.handleCreateItem(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Individual item – Go 1.22+ pattern matching not available on all targets;
	// use prefix strip instead.
	mux.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/items/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			a.handleGetItem(w, r, id)
		case http.MethodPut:
			a.handleUpdateItem(w, r, id)
		case http.MethodDelete:
			a.handleDeleteItem(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Deliberate error endpoints
	mux.HandleFunc("/errors/unauthorized", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
	})
	mux.HandleFunc("/errors/forbidden", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, "forbidden", "access denied")
	})
	mux.HandleFunc("/errors/rate-limited", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "too_many_requests", "rate limit exceeded")
	})
	mux.HandleFunc("/errors/internal", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusInternalServerError, "internal_error", "unexpected server error")
	})

	// Long-running operations
	mux.HandleFunc("/operations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleCreateOperation(w, r)
	})
	mux.HandleFunc("/operations/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/operations/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleGetOperation(w, r, id)
	})
}

// --- item handlers ---

type listResponse struct {
	Items      []*Item `json:"items"`
	Total      int     `json:"total"`
	Page       int     `json:"page"`
	PageSize   int     `json:"pageSize"`
	TotalPages int     `json:"totalPages"`
}

func (a *App) handleListItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tag := q.Get("tag")
	page := intParam(q.Get("page"), 1)
	pageSize := intParam(q.Get("pageSize"), 20)

	all := a.store.listItems(tag)
	total := len(all)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	writeJSON(w, http.StatusOK, listResponse{
		Items:      all[start:end],
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

func (a *App) handleGetItem(w http.ResponseWriter, _ *http.Request, id string) {
	it, ok := a.store.getItem(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "item not found")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

type itemInput struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

func (a *App) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	var inp itemInput
	if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if inp.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "name is required")
		return
	}
	it := a.store.createItem(inp.Name, inp.Tags)
	writeJSON(w, http.StatusCreated, it)
}

func (a *App) handleUpdateItem(w http.ResponseWriter, r *http.Request, id string) {
	var inp itemInput
	if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	it, ok := a.store.updateItem(id, inp.Name, inp.Tags)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "item not found")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (a *App) handleDeleteItem(w http.ResponseWriter, _ *http.Request, id string) {
	if !a.store.deleteItem(id) {
		writeError(w, http.StatusNotFound, "not_found", "item not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- operation handlers ---

func (a *App) handleCreateOperation(w http.ResponseWriter, _ *http.Request) {
	op := a.store.createOperation()
	writeJSON(w, http.StatusAccepted, op)
}

func (a *App) handleGetOperation(w http.ResponseWriter, _ *http.Request, id string) {
	op, ok := a.store.getOperation(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "operation not found")
		return
	}
	writeJSON(w, http.StatusOK, op)
}

// --- helpers ---

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Code: code, Message: message})
}

func intParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return def
	}
	return v
}
