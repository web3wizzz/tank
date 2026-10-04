package metadata

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNoRegistrationJob = errors.New("no registration job is ready")

type RegistrationJob struct {
	FileID          string
	Target          string
	TransactionHash string
	Attempts        int
	Token           string
}

type RegistrationStatus struct {
	State           string
	TransactionHash string
	Attempts        int
	LastError       string
}

// Target identifies a chain, contract, and registrant.
// Separate targets maintain separate registration records.
func validTarget(target string) bool {
	return len(target) > 0 &&
		len(target) <= 256 &&
		strings.TrimSpace(target) == target
}

func (s *Store) EnqueueRegistration(
	ctx context.Context,
	id, target string,
) error {
	if !validTarget(target) {
		return fmt.Errorf("invalid registration target")
	}
	if _, err := s.Load(ctx, id); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO registration_jobs(file_id, target, next_attempt)
		VALUES (?, ?, ?)
		ON CONFLICT(file_id, target) DO NOTHING
	`, id, target, time.Now().UnixMilli())

	return err
}

func (s *Store) ClaimRegistration(
	ctx context.Context,
	target string,
	lease time.Duration,
) (RegistrationJob, error) {
	var job RegistrationJob

	if !validTarget(target) {
		return job, fmt.Errorf("invalid registration target")
	}
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
		UPDATE registration_jobs
		SET lease_token = ?,
		    lease_until = ?,
		    attempts = attempts + 1
		WHERE target = ? AND file_id = (
			SELECT file_id
			FROM registration_jobs
			WHERE target = ?
			  AND status IN ('pending', 'submitted')
			  AND next_attempt <= ?
			  AND lease_until <= ?
			ORDER BY next_attempt, file_id
			LIMIT 1
		)
		RETURNING file_id, target, tx_hash, attempts, lease_token
	`,
		token,
		now.Add(lease).UnixMilli(),
		target,
		target,
		now.UnixMilli(),
		now.UnixMilli(),
	).Scan(
		&job.FileID,
		&job.Target,
		&job.TransactionHash,
		&job.Attempts,
		&job.Token,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return RegistrationJob{}, ErrNoRegistrationJob
	}

	return job, err
}

// SaveRegistrationTransaction preserves the transaction hash for recovery.
// A different transaction cannot replace an already saved hash.
func (s *Store) SaveRegistrationTransaction(
	ctx context.Context,
	job RegistrationJob,
	hash string,
) error {
	if len(hash) != 66 || !strings.HasPrefix(hash, "0x") ||
		!validHash(hash[2:]) {
		return fmt.Errorf("invalid transaction hash")
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE registration_jobs
		SET tx_hash = ?, status = 'submitted'
		WHERE file_id = ? AND target = ?
		  AND lease_token = ? AND lease_until > ?
		  AND (tx_hash = '' OR tx_hash = ?)
	`,
		hash,
		job.FileID,
		job.Target,
		job.Token,
		time.Now().UnixMilli(),
		hash,
	)
	if err != nil {
		return err
	}

	return checkLeaseResult(result)
}

func (s *Store) CompleteRegistration(
	ctx context.Context,
	job RegistrationJob,
) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE registration_jobs
		SET status = 'registered',
		    lease_until = 0,
		    lease_token = '',
		    last_error = ''
		WHERE file_id = ? AND target = ?
		  AND lease_token = ? AND lease_until > ?
	`, job.FileID, job.Target, job.Token, time.Now().UnixMilli())
	if err != nil {
		return err
	}

	return checkLeaseResult(result)
}

func (s *Store) RetryRegistration(
	ctx context.Context,
	job RegistrationJob,
	delay time.Duration,
	reason string,
) error {
	if delay < time.Second || delay > 24*time.Hour {
		return fmt.Errorf("retry delay must be between one second and 24 hours")
	}
	if len(reason) > 1024 {
		reason = reason[:1024]
	}

	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE registration_jobs
		SET next_attempt = ?,
		    lease_until = 0,
		    lease_token = '',
		    last_error = ?
		WHERE file_id = ? AND target = ?
		  AND lease_token = ? AND lease_until > ?
	`,
		now.Add(delay).UnixMilli(),
		reason,
		job.FileID,
		job.Target,
		job.Token,
		now.UnixMilli(),
	)
	if err != nil {
		return err
	}

	return checkLeaseResult(result)
}

func (s *Store) FailRegistration(
	ctx context.Context,
	job RegistrationJob,
	reason string,
) error {
	if len(reason) > 1024 {
		reason = reason[:1024]
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE registration_jobs
		SET status = 'failed',
		    lease_until = 0,
		    lease_token = '',
		    last_error = ?
		WHERE file_id = ? AND target = ?
		  AND lease_token = ? AND lease_until > ?
	`, reason, job.FileID, job.Target, job.Token, time.Now().UnixMilli())
	if err != nil {
		return err
	}

	return checkLeaseResult(result)
}

func (s *Store) GetRegistrationStatus(
	ctx context.Context,
	id, target string,
) (RegistrationStatus, error) {
	var status RegistrationStatus
	if !validHash(id) || !validTarget(target) {
		return status, fmt.Errorf("invalid file ID or target")
	}

	err := s.db.QueryRowContext(ctx, `
		SELECT status, tx_hash, attempts, last_error
		FROM registration_jobs
		WHERE file_id = ? AND target = ?
	`, id, target).Scan(
		&status.State,
		&status.TransactionHash,
		&status.Attempts,
		&status.LastError,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return status, ErrNotFound
	}

	return status, err
}
