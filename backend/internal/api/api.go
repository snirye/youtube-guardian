package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type databaseHealth interface {
	PingContext(context.Context) error
}

func New(db databaseHealth, token string) http.Handler {
	tokenHash := sha256.Sum256([]byte(token))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := db.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		suppliedHash := sha256.Sum256([]byte(value))
		if !ok || !strings.EqualFold(scheme, "Bearer") || value == "" || subtle.ConstantTimeCompare(suppliedHash[:], tokenHash[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
