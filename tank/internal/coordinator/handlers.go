package coordinator

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

func NewHandler(service *Service, token string) (http.Handler, error) {
	if service == nil || len(token) < 32 {
		return nil, fmt.Errorf("service and API token of at least 32 characters required")
	}

	expectedToken := sha256.Sum256([]byte(token))
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})

	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			actual := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
			if subtle.ConstantTimeCompare(actual[:], expectedToken[:]) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("POST /tank", auth(func(w http.ResponseWriter, r *http.Request) {
		// The request body is the raw file, not multipart form data.
		r.Body = http.MaxBytesReader(w, r.Body, MaxFileBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "file exceeds 16 MiB", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "cannot read file", http.StatusBadRequest)
			}
			return
		}

		m, err := service.Tank(r.Context(), data)
		if errors.Is(err, ErrInvalidFile) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			log.Printf("[Tank] tank failed: %v", err)
			http.Error(w, "could not store file", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m)
	}))

	mux.HandleFunc("GET /retrieve/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		hash, err := integrity.ParseHash(id)
		if err != nil || hash.String() != id {
			http.Error(w, "invalid file ID", http.StatusBadRequest)
			return
		}

		data, err := service.Retrieve(r.Context(), id)
		if errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Printf("[Tank] retrieve failed: %v", err)
			http.Error(w, "file unavailable", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="tank-file"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	}))

	mux.HandleFunc("GET /list", auth(func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		if after != "" {
			hash, err := integrity.ParseHash(after)
			if err != nil || hash.String() != after {
				http.Error(w, "invalid cursor", http.StatusBadRequest)
				return
			}
		}

		ids, err := service.List(r.Context(), after)
		if err != nil {
			http.Error(w, "could not list files", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ids)
	}))

	return mux, nil
}
