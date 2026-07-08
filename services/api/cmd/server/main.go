package main

import (
	"log"
	"net/http"

	"github.com/nasraldin/turbo-cache-forge/services/api/internal/server"
)

func main() {
	srv := server.New(server.Deps{})
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", srv); err != nil {
		log.Fatal(err)
	}
}
