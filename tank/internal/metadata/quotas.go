package metadata

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrUploadBusy = errors.New("another upload is in progress")

type StorageUsage struct {
	UserBytes  int64
	TotalBytes int64
}

// StorageUsage counts logical committed file bytes. Shared files are counted once
// globally and once for each user granted access; repeated uploads are not charged.
func (s *Store) StorageUsage(ctx context.Context, principalID string) (StorageUsage, error) {
	var usage StorageUsage
	err := s.db.QueryRowContext(ctx, `
  SELECT COALESCE((SELECT SUM(json_extract(m.body,'$.size')) FROM manifests m
   JOIN file_access a ON a.file_id=m.file_id WHERE a.principal_id=?),0),
   COALESCE((SELECT SUM(json_extract(body,'$.size')) FROM manifests),0)
 `, principalID).Scan(&usage.UserBytes, &usage.TotalBytes)
	if err == nil && (usage.UserBytes < 0 || usage.TotalBytes < 0) {
		err = fmt.Errorf("invalid storage accounting")
	}
	return usage, err
}

// AcquireUploadLease serializes quota checks, manifest creation, and permission
// grants across coordinator processes sharing this database. Callers must enforce
// a request deadline; the lease outlives that deadline to fence a crashed owner.
func (s *Store) AcquireUploadLease(ctx context.Context) (string, error) {
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(time.Now().Add(5*time.Minute)) {
		return "", fmt.Errorf("bounded upload context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	token, err := accessRandomHex(16)
	if err != nil {
		return "", err
	}
	result, err := s.db.ExecContext(ctx, `
  INSERT INTO upload_leases (name,lease_token,lease_until) VALUES ('storage',?,?)
  ON CONFLICT(name) DO UPDATE SET lease_token=excluded.lease_token,lease_until=excluded.lease_until
  WHERE upload_leases.lease_until<=?
 `, token, deadline.Add(30*time.Second).Unix(), time.Now().Unix())
	if err != nil {
		return "", err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", ErrUploadBusy
	}
	return token, nil
}

func (s *Store) ReleaseUploadLease(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM upload_leases WHERE name='storage' AND lease_token=?", token)
	return err
}
