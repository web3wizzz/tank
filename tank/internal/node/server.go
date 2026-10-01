package node

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"tank.local/tank/internal/encoding"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/storage"
)

func NewHandler(backend storage.Backend, token string) (http.Handler, error) {
	if len(token) < 32 {
		return nil, fmt.Errorf("node token must contain at least 32 characters")
	}

	expectedToken := sha256.Sum256([]byte(token))
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
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

	mux.HandleFunc("PUT /shards/{file}/{segment}/{index}", auth(
		func(w http.ResponseWriter, r *http.Request) {
			key, err := requestKey(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			expected, err := integrity.ParseHash(r.Header.Get("X-Tank-Shard-Hash"))
			if err != nil {
				http.Error(w, "valid X-Tank-Shard-Hash required", http.StatusBadRequest)
				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, storage.MaxShardBytes)
			data, err := io.ReadAll(r.Body)
			if err != nil {
				var sizeError *http.MaxBytesError
				if errors.As(err, &sizeError) {
					http.Error(w, "shard too large", http.StatusRequestEntityTooLarge)
				} else {
					http.Error(w, "cannot read shard", http.StatusBadRequest)
				}
				return
			}

			if len(data) == 0 {
				http.Error(w, "empty shard", http.StatusBadRequest)
				return
			}
			if integrity.Digest(data) != expected {
				http.Error(w, "shard hash mismatch", http.StatusUnprocessableEntity)
				return
			}

			if err := backend.Put(r.Context(), key, data, expected); err != nil {
				http.Error(w, "storage failed", http.StatusInternalServerError)
				return
			}

			w.Header().Set("X-Tank-Shard-Hash", expected.String())
			w.WriteHeader(http.StatusCreated)
		},
	))

	mux.HandleFunc("GET /shards/{file}/{segment}/{index}", auth(
		func(w http.ResponseWriter, r *http.Request) {
			key, err := requestKey(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			data, err := backend.Get(r.Context(), key)
			if errors.Is(err, storage.ErrShardNotFound) {
				http.Error(w, "shard not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "retrieval failed", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Tank-Shard-Hash", integrity.Digest(data).String())
			w.Write(data)
		},
	))

	return mux, nil
}

func requestKey(r *http.Request) (storage.ShardKey, error) {
	id := r.PathValue("file")
	hash, err := integrity.ParseHash(id)
	if err != nil || hash.String() != id {
		return storage.ShardKey{}, fmt.Errorf("invalid file ID")
	}

	segment, err := strconv.Atoi(r.PathValue("segment"))
	if err != nil || segment < 0 {
		return storage.ShardKey{}, fmt.Errorf("invalid segment index")
	}

	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 || index >= encoding.TotalShards {
		return storage.ShardKey{}, fmt.Errorf("invalid shard index")
	}

	return storage.ShardKey{
		FileID:  id,
		Segment: segment,
		Index:   index,
	}, nil
}
