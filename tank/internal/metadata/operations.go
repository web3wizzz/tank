package metadata

import "context"

type Operations struct {
	Files                 int64 `json:"files"`
	CommittedBytes        int64 `json:"committed_bytes"`
	RepairJobs            int64 `json:"repair_jobs"`
	RegistrationPending   int64 `json:"registration_pending"`
	RegistrationSubmitted int64 `json:"registration_submitted"`
	RegistrationFailed    int64 `json:"registration_failed"`
	RegistrationComplete  int64 `json:"registration_complete"`
}

func (s *Store) Operations(ctx context.Context) (Operations, error) {
	var result Operations
	err := s.db.QueryRowContext(ctx, `SELECT
  (SELECT count(*) FROM manifests),
  COALESCE((SELECT SUM(json_extract(body,'$.size')) FROM manifests),0),
  (SELECT count(*) FROM repair_jobs),
  (SELECT count(*) FROM registration_jobs WHERE status='pending'),
  (SELECT count(*) FROM registration_jobs WHERE status='submitted'),
  (SELECT count(*) FROM registration_jobs WHERE status='failed'),
  (SELECT count(*) FROM registration_jobs WHERE status='registered')
 `).Scan(&result.Files, &result.CommittedBytes, &result.RepairJobs, &result.RegistrationPending, &result.RegistrationSubmitted, &result.RegistrationFailed, &result.RegistrationComplete)
	return result, err
}
