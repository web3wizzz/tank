package metadata

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNoRepairJob = errors.New("no repair job is ready")
	ErrLeaseLost   = errors.New("repair job lease expired or changed")
)

type RepairJob struct {
	FileID   string
	Attempts int
	Token    string
}

// EnqueueRepair is idempotent: one outstanding job per file.
func (s *Store) EnqueueRepair(ctx context.Context, id string) error {
	if _, err := s.Load(ctx, id); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO repair_jobs(file_id, next_attempt)
		VALUES (?, ?)
		ON CONFLICT(file_id) DO NOTHING
	`, id, time.Now().UnixMilli())

	return err
}

// ClaimRepair atomically claims one due job.
// An abandoned job becomes eligible after its lease expires.
func (s *Store) ClaimRepair(
	ctx context.Context,
	lease time.Duration,
) (RepairJob, error) {
	var job RepairJob
	if lease < time.Second || lease > time.Hour {
		return job, fmt.Errorf("lease must be between one second and one hour")
	}

	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return job, err
	}
	token := hex.EncodeToString(random[:])
	now := time.Now()

	err := s.db.QueryRowContext(ctx, `
		UPDATE repair_jobs
		SET lease_token = ?,
		    lease_until = ?,
		    attempts = attempts + 1
		WHERE file_id = (
			SELECT file_id
			FROM repair_jobs
			WHERE next_attempt <= ? AND lease_until <= ?
			ORDER BY next_attempt, file_id
			LIMIT 1
		)
		RETURNING file_id, attempts, lease_token
	`,
		token,
		now.Add(lease).UnixMilli(),
		now.UnixMilli(),
		now.UnixMilli(),
	).Scan(&job.FileID, &job.Attempts, &job.Token)

	if errors.Is(err, sql.ErrNoRows) {
		return RepairJob{}, ErrNoRepairJob
	}

	return job, err
}

func (s *Store) CompleteRepair(ctx context.Context, job RepairJob) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM repair_jobs
		WHERE file_id = ?
		  AND lease_token = ?
		  AND lease_until > ?
	`, job.FileID, job.Token, time.Now().UnixMilli())
	if err != nil {
		return err
	}

	return checkLeaseResult(result)
}

// RetryRepair releases a job and schedules another attempt.
func (s *Store) RetryRepair(
	ctx context.Context,
	job RepairJob,
	delay time.Duration,
	reason string,
) error {
	if delay < time.Second || delay > 24*time.Hour {
		return fmt.Errorf("retry delay must be between one second and 24 hours")
	}

	// Keep stored error messages bounded.
	if len(reason) > 1024 {
		reason = reason[:1024]
	}

	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE repair_jobs
		SET next_attempt = ?,
		    lease_until = 0,
		    lease_token = '',
		    last_error = ?
		WHERE file_id = ?
		  AND lease_token = ?
		  AND lease_until > ?
	`,
		now.Add(delay).UnixMilli(),
		reason,
		job.FileID,
		job.Token,
		now.UnixMilli(),
	)
	if err != nil {
		return err
	}

	return checkLeaseResult(result)
}

func checkLeaseResult(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}
