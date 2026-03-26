package httpapi

import "net/http"

type Dependencies struct {
}

func StubDependencies() Dependencies {
	return Dependencies{}
}

func RegisterRoutes(mux *http.ServeMux, deps Dependencies) http.Handler {
	mux.HandleFunc("/v1/admin/me", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	return mux
}
