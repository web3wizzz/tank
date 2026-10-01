package coordinator

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

func (s *Service) handleRepair(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	hash, err := integrity.ParseHash(id)
	if err != nil || hash.String() != id {
		http.Error(w, "invalid file ID", http.StatusBadRequest)
		return
	}

	var request struct {
		ReplacementURL string `json:"replacement_url"`
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid repair request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "expected one JSON object", http.StatusBadRequest)
		return
	}

	if _, exists := s.byURL[request.ReplacementURL]; !exists {
		http.Error(w, "replacement node is not configured", http.StatusBadRequest)
		return
	}

	count, err := s.Repair(r.Context(), id, request.ReplacementURL)
	switch {
	case errors.Is(err, metadata.ErrNotFound):
		http.Error(w, "file not found", http.StatusNotFound)
		return
	case errors.Is(err, metadata.ErrManifestConflict):
		http.Error(w, "manifest changed; retry repair", http.StatusConflict)
		return
	case errors.Is(err, ErrUnavailable):
		http.Error(w, "insufficient shards for repair", http.StatusServiceUnavailable)
		return
	case err != nil:
		log.Printf("[Tank] repair failed: %v", err)
		http.Error(w, "repair failed; check coordinator logs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"file_id":         id,
		"repaired_shards": count,
	})
}
