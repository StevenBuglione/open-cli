package main

import (
	"log"
	"net/http"
	"os"

	"github.com/StevenBuglione/open-cli/product-tests/services/oauthstub/internal/server"
)

func main() {
	addr := os.Getenv("OAUTHSTUB_ADDR")
	if addr == "" {
		addr = ":9090"
	}
	issuer := os.Getenv("OAUTHSTUB_ISSUER")
	if issuer == "" {
		issuer = "http://localhost" + addr
	}
	store := server.NewStore()
	srv := server.New(store, issuer)
	log.Printf("oauthstub listening on %s (issuer: %s)", addr, issuer)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
