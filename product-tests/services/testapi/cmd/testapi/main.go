package main

import (
	"log"
	"net/http"
	"os"

	"github.com/StevenBuglione/open-cli/product-tests/services/testapi/internal/app"
)

func main() {
	addr := os.Getenv("TESTAPI_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	a := app.New()
	log.Printf("testapi listening on %s", addr)
	if err := http.ListenAndServe(addr, a.Handler()); err != nil {
		log.Fatal(err)
	}
}
