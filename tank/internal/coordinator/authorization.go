package coordinator

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/limits"
	"tank.local/tank/internal/metadata"
)

type identityContextKey struct{}

type requestIdentity struct {
	Admin       bool
	PrincipalID string
	File        metadata.FileAccess
}

func identityFromRequest(r *http.Request) (requestIdentity, bool) {
	value, ok := r.Context().Value(identityContextKey{}).(requestIdentity)
	return value, ok
}

type authorizer struct {
	governor  *limits.Governor
	store     *metadata.Store
	adminHash [32]byte
}

func newAuthorizer(store *metadata.Store, adminToken string, governors ...*limits.Governor) *authorizer {
	var governor *limits.Governor
	if len(governors) > 0 {
		governor = governors[0]
	}
	return &authorizer{
		governor:  governor,
		store:     store,
		adminHash: sha256.Sum256([]byte(adminToken)),
	}
}

func (a *authorizer) middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if a.governor != nil {
			release, ok := a.governor.Begin()
			if !ok {
				limitedRequest(w)
				return
			}
			defer release()
			ctx, cancel := context.WithTimeout(r.Context(), a.governor.Timeout())
			defer cancel()
			r = r.WithContext(ctx)
			// Interrupt slow body reads at the configured deadline (capped at 30s).
			controller := http.NewResponseController(w)
			deadline := time.Now().Add(30 * time.Second)
			if requestDeadline, ok := ctx.Deadline(); ok && requestDeadline.Before(deadline) {
				deadline = requestDeadline
			}
			if err := controller.SetReadDeadline(deadline); err == nil {
				defer func() {
					if time.Now().Before(deadline) {
						_ = controller.SetReadDeadline(time.Time{})
					}
				}()
			}

		}

		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(header, "Bearer ")
		hash := sha256.Sum256([]byte(token))
		identity := requestIdentity{}

		if subtle.ConstantTimeCompare(hash[:], a.adminHash[:]) == 1 {
			identity.Admin = true
		} else {
			principal, err := a.store.AuthenticateAccessKey(r.Context(), token)
			if errors.Is(err, metadata.ErrUnauthorized) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if err != nil {
				http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
				return
			}
			identity.PrincipalID = principal.ID
		}

		if a.governor != nil {
			key := identity.PrincipalID
			if identity.Admin {
				key = "administrator"
			}
			release, ok := a.governor.AdmitUser(key)
			if !ok {
				limitedRequest(w)
				return
			}
			defer release()
		}
		if !identity.Admin {
			switch r.Pattern {
			case "POST /tank", "GET /list":
				// Tanking grants access only after the bytes have been stored.
				// Listing is scoped inside its handler.

			case "GET /retrieve/{id}",
				"GET /files/{id}/info",
				"GET /registrations/{id}":
				id := r.PathValue("id")
				parsed, err := integrity.ParseHash(id)
				if err != nil || parsed.String() != id {
					http.Error(w, "invalid file ID", http.StatusBadRequest)
					return
				}

				access, err := a.store.LoadFileAccess(
					r.Context(), identity.PrincipalID, id,
				)
				if errors.Is(err, metadata.ErrAccessDenied) ||
					errors.Is(err, metadata.ErrNotFound) {
					// Do not disclose whether another user's file exists.
					http.Error(w, "file not found", http.StatusNotFound)
					return
				}
				if err != nil {
					http.Error(w, "authorization unavailable", http.StatusServiceUnavailable)
					return
				}
				identity.File = access

			default:
				// Includes manual repair and future protected routes.
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}

		ctx := context.WithValue(r.Context(), identityContextKey{}, identity)
		next(w, r.WithContext(ctx))
	}
}

func requestFilename(
	r *http.Request, store *metadata.Store, id string,
) (string, error) {
	identity, ok := identityFromRequest(r)
	if !ok {
		return "", metadata.ErrAccessDenied
	}
	if !identity.Admin {
		return identity.File.Filename, nil
	}
	return store.LoadFilename(r.Context(), id)
}

func limitedRequest(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "request limit reached; retry later", http.StatusTooManyRequests)
}
