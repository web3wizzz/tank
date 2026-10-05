package coordinator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

func NewHandler(service *Service, token string) (http.Handler, error) {
	if service == nil || len(token) < 32 {
		return nil, fmt.Errorf("service and API token of at least 32 characters required")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})

	auth := newAuthorizer(service.store, token).middleware

	mux.HandleFunc("POST /tank", auth(func(w http.ResponseWriter, r *http.Request) {

		// Optional percent-encoded filename. File bytes remain the raw body.
		var filename string
		if encoded := r.Header.Get("X-Tank-Filename"); encoded != "" {
			if len(encoded) > 1024 {
				http.Error(w, "filename too long", http.StatusBadRequest)
				return
			}
			decoded, err := url.PathUnescape(encoded)
			if err != nil {
				http.Error(w, "invalid filename encoding", http.StatusBadRequest)
				return
			}
			filename, err = metadata.NormalizeFilename(decoded)
			if err != nil {
				http.Error(w, "invalid filename", http.StatusBadRequest)
				return
			}
		}

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

		identity, ok := identityFromRequest(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if !identity.Admin {
			if err := service.store.GrantFileAccess(
				r.Context(), identity.PrincipalID, m.FileID, filename,
			); err != nil {
				http.Error(w, "could not save file permission", http.StatusServiceUnavailable)
				return
			}
		} else if filename != "" {
			if err := service.store.SaveFilename(r.Context(), m.FileID, filename); err != nil {
				http.Error(w, "could not save filename", http.StatusServiceUnavailable)
				return
			}
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

		filename, err := requestFilename(r, service.store, id)
		if err != nil {
			http.Error(w, "filename unavailable", http.StatusServiceUnavailable)
			return
		}
		if filename == "" {
			filename = id + ".bin"
		}
		w.Header().Set("Content-Disposition",
			mime.FormatMediaType("attachment", map[string]string{"filename": filename}))

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

		var ids []string
		var err error
		identity, ok := identityFromRequest(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if identity.Admin {
			ids, err = service.List(r.Context(), after)
		} else {
			ids, err = service.store.ListAccessibleFiles(
				r.Context(), identity.PrincipalID, after,
			)
		}
		if err != nil {
			http.Error(w, "could not list files", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ids)
	}))

	mux.HandleFunc("GET /files/{id}/info", auth(service.handleFileInfo))

	mux.HandleFunc("POST /repair/{id}", auth(service.handleRepair))

	return mux, nil
}
