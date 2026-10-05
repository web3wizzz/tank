package coordinator

import (
	"encoding/json"
	"errors"
	"net/http"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

func (s *Service) handleFileInfo(w http.ResponseWriter, r *http.Request) {
	identity, ok := identityFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !identity.Admin {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(identity.File)
		return
	}

	id := r.PathValue("id")
	hash, err := integrity.ParseHash(id)
	if err != nil || hash.String() != id {
		http.Error(w, "invalid file ID", http.StatusBadRequest)
		return
	}

	m, err := s.store.Load(r.Context(), id)
	if errors.Is(err, metadata.ErrNotFound) {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "file metadata unavailable", http.StatusServiceUnavailable)
		return
	}

	name, err := s.store.LoadFilename(r.Context(), id)
	if err != nil {
		http.Error(w, "filename unavailable", http.StatusServiceUnavailable)
		return
	}
	if name == "" {
		name = id + ".bin"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(struct {
		FileID   string `json:"file_id"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}{id, name, m.Size})
}
