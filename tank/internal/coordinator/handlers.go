package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/storage"
)

func NewHandler(service *Service, token string) (http.Handler, error) {
	if service == nil || len(token) < 32 {
		return nil, fmt.Errorf("service and API token of at least 32 characters required")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})

	auth := newAuthorizer(service.store, token, service.governor).middleware

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
		if r.ContentLength > service.resourceLimits.MaxFileBytes {
			http.Error(w, "file exceeds configured upload limit", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, service.resourceLimits.MaxFileBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "file exceeds configured upload limit", http.StatusRequestEntityTooLarge)
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				http.Error(w, "upload read deadline exceeded", http.StatusRequestTimeout)
			} else {
				http.Error(w, "cannot read file", http.StatusBadRequest)
			}
			return
		}

		if len(data) == 0 {
			http.Error(w, "file is empty", http.StatusBadRequest)
			return
		}
		identity, ok := identityFromRequest(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		lease, err := service.store.AcquireUploadLease(r.Context())
		if errors.Is(err, metadata.ErrUploadBusy) {
			limitedRequest(w)
			return
		}
		if err != nil {
			http.Error(w, "storage admission unavailable", http.StatusServiceUnavailable)
			return
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := service.store.ReleaseUploadLease(ctx, lease); err != nil {
				log.Printf("[Tank] upload lease cleanup failed: %v", err)
			}
		}()
		usage, err := service.store.StorageUsage(r.Context(), identity.PrincipalID)
		if err != nil {
			http.Error(w, "storage accounting unavailable", http.StatusServiceUnavailable)
			return
		}
		id := integrity.Digest(data).String()
		globalCharge := int64(len(data))
		if _, err := service.store.Load(r.Context(), id); err == nil {
			globalCharge = 0
		} else if !errors.Is(err, metadata.ErrNotFound) {
			http.Error(w, "metadata unavailable", http.StatusServiceUnavailable)
			return
		}
		userCharge := int64(len(data))
		if !identity.Admin {
			if _, err := service.store.LoadFileAccess(r.Context(), identity.PrincipalID, id); err == nil {
				userCharge = 0
			} else if !errors.Is(err, metadata.ErrAccessDenied) && !errors.Is(err, metadata.ErrNotFound) {
				http.Error(w, "permission accounting unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		if service.resourceLimits.TotalStorageBytes > 0 && globalCharge > 0 && usage.TotalBytes > service.resourceLimits.TotalStorageBytes-globalCharge {
			http.Error(w, "total storage quota exceeded", http.StatusInsufficientStorage)
			return
		}
		if !identity.Admin && service.resourceLimits.UserStorageBytes > 0 && userCharge > 0 && usage.UserBytes > service.resourceLimits.UserStorageBytes-userCharge {
			http.Error(w, "user storage quota exceeded", http.StatusInsufficientStorage)
			return
		}
		m, err := service.Tank(r.Context(), data)
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "storage request deadline exceeded", http.StatusGatewayTimeout)
			return
		}
		if errors.Is(err, storage.ErrQuotaExceeded) {
			http.Error(w, "node storage capacity exceeded", http.StatusInsufficientStorage)
			return
		}
		if errors.Is(err, ErrInvalidFile) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			log.Printf("[Tank] tank failed: %v", err)
			http.Error(w, "could not store file", http.StatusServiceUnavailable)
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
