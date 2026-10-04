package coordinator

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"tank.local/tank/internal/registry"
)

func NewHandlerWithRegistration(
	service *Service,
	store *metadata.Store,
	token, rpcURL, contract, registrant string,
) (http.Handler, error) {
	base, err := NewHandler(service, token)
	if err != nil {
		return nil, err
	}

	// Storage can also run without chain configuration.
	if rpcURL == "" && contract == "" && registrant == "" {
		return base, nil
	}

	worker, err := registry.NewWorker(store, registry.Config{
		RPCURL:     rpcURL,
		Contract:   contract,
		Registrant: registrant,
	})
	if err != nil {
		return nil, err
	}
	target := worker.Target()
	expectedAuth := sha256.Sum256([]byte("Bearer " + token))

	mux := http.NewServeMux()
	mux.Handle("/", base)
	mux.HandleFunc("GET /registrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		actualAuth := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(actualAuth[:], expectedAuth[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		id := r.PathValue("id")
		hash, err := integrity.ParseHash(id)
		if err != nil || hash.String() != id {
			http.Error(w, "invalid file ID", http.StatusBadRequest)
			return
		}

		if _, err := store.Load(r.Context(), id); err != nil {
			if errors.Is(err, metadata.ErrNotFound) {
				http.Error(w, "file not found", http.StatusNotFound)
			} else {
				http.Error(w, "metadata unavailable", http.StatusInternalServerError)
			}
			return
		}

		status, err := store.GetRegistrationStatus(r.Context(), id, target)
		if errors.Is(err, metadata.ErrNotFound) {
			status = metadata.RegistrationStatus{State: "not_queued"}
		} else if err != nil {
			http.Error(w, "registration status unavailable", http.StatusInternalServerError)
			return
		}

		response := struct {
			FileID          string `json:"file_id"`
			Target          string `json:"target"`
			Status          string `json:"status"`
			TransactionHash string `json:"transaction_hash,omitempty"`
			Attempts        int    `json:"attempts"`
			LastError       string `json:"last_error,omitempty"`
		}{
			FileID:          id,
			Target:          target,
			Status:          status.State,
			TransactionHash: status.TransactionHash,
			Attempts:        status.Attempts,
			LastError:       status.LastError,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	return mux, nil
}
