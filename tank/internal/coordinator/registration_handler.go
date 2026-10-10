package coordinator

import (
	"encoding/json"
	"errors"
	"fmt"
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
	if store != service.store {
		return nil, fmt.Errorf("registration and storage must use the same metadata store")
	}

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

	return newRegistrationHandler(
		base, service, store, token, worker.Target(),
	), nil
}

func newRegistrationHandler(
	base http.Handler,
	service *Service,
	store *metadata.Store,
	token, target string,
) http.Handler {
	auth := newAuthorizer(service.store, token, service.governor).middleware
	mux := http.NewServeMux()
	mux.Handle("/", base)

	mux.HandleFunc("GET /registrations/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
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

		identity, _ := identityFromRequest(r)
		if !identity.Admin {
			// Worker errors can contain internal infrastructure details.
			response.LastError = ""
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))

	return mux
}
