package metadata

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"strconv"
	"strings"
	"time"
)

var ErrAccountBusy = errors.New("signer account already has an outstanding transaction")
var ErrNonceConflict = errors.New("signer nonce changed; operator review required")

type PreparedRegistration struct {
	ChainID int64
	Account string
	FileID  string
	Target  string
	Nonce   uint64
	Hash    string
	Raw     []byte
}

func validAccount(account string) bool {
	return len(account) == 42 && strings.HasPrefix(account, "0x") && account == strings.ToLower(account) && account != "0x"+strings.Repeat("0", 40) && validHash(account[2:]+strings.Repeat("0", 24))
}
func scanPrepared(row interface{ Scan(...any) error }) (PreparedRegistration, string, error) {
	var value PreparedRegistration
	var nonce, state string
	err := row.Scan(&value.ChainID, &value.Account, &value.FileID, &value.Target, &nonce, &value.Hash, &value.Raw, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return value, "", ErrNotFound
	}
	if err != nil {
		return value, "", err
	}
	value.Nonce, err = strconv.ParseUint(nonce, 10, 64)
	return value, state, err
}
func registrationLease(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, job RegistrationJob) error {
	var count int
	err := query.QueryRowContext(ctx, `SELECT count(*) FROM registration_jobs WHERE file_id=? AND target=? AND lease_token=? AND lease_until>? AND status IN ('pending','submitted')`, job.FileID, job.Target, job.Token, time.Now().UnixMilli()).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseLost
	}
	return nil
}

// PreparedRegistration reads only under the current job fence. Process lease
// expiry never clears an outstanding account transaction.
func (s *Store) PreparedRegistration(ctx context.Context, job RegistrationJob, chainID int64, account string) (PreparedRegistration, error) {
	var empty PreparedRegistration
	if err := registrationLease(ctx, s.db, job); err != nil {
		return empty, err
	}
	value, state, err := scanPrepared(s.db.QueryRowContext(ctx, `SELECT chain_id,account,file_id,target,nonce,tx_hash,raw_tx,state FROM registration_transactions WHERE chain_id=? AND account=?`, chainID, account))
	if err != nil {
		return empty, err
	}
	if state == "confirmed" {
		return empty, ErrNotFound
	}
	if value.FileID != job.FileID || value.Target != job.Target {
		return empty, ErrAccountBusy
	}
	return value, nil
}

// SavePreparedRegistration atomically fences the job and exclusive account lane,
// recording signed bytes before any caller can submit them. It never broadcasts.
func (s *Store) SavePreparedRegistration(ctx context.Context, job RegistrationJob, value PreparedRegistration) error {
	if value.ChainID <= 0 || !validAccount(value.Account) || value.FileID != job.FileID || value.Target != job.Target || !strings.HasPrefix(value.Target, strconv.FormatInt(value.ChainID, 10)+":") || !strings.HasSuffix(value.Target, ":"+value.Account) || len(value.Raw) == 0 || len(value.Raw) > 4096 || value.Hash != strings.ToLower(crypto.Keccak256Hash(value.Raw).Hex()) {
		return fmt.Errorf("invalid prepared transaction")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE registration_jobs SET tx_hash=? WHERE file_id=? AND target=? AND lease_token=? AND lease_until>? AND status IN ('pending','submitted') AND (tx_hash='' OR tx_hash=?)`, value.Hash, job.FileID, job.Target, job.Token, time.Now().UnixMilli(), value.Hash)
	if err != nil {
		return err
	}
	if err := checkLeaseResult(result); err != nil {
		return err
	}
	existing, state, err := scanPrepared(tx.QueryRowContext(ctx, `SELECT chain_id,account,file_id,target,nonce,tx_hash,raw_tx,state FROM registration_transactions WHERE chain_id=? AND account=?`, value.ChainID, value.Account))
	switch {
	case errors.Is(err, ErrNotFound):
		_, err = tx.ExecContext(ctx, `INSERT INTO registration_transactions(chain_id,account,file_id,target,nonce,tx_hash,raw_tx,state) VALUES(?,?,?,?,?,?,?,'prepared')`, value.ChainID, value.Account, value.FileID, value.Target, strconv.FormatUint(value.Nonce, 10), value.Hash, value.Raw)
	case err != nil:
		return err
	case state == "prepared":
		if existing.FileID != value.FileID || existing.Target != value.Target {
			return ErrAccountBusy
		}
		if existing.Nonce != value.Nonce || existing.Hash != value.Hash || !bytes.Equal(existing.Raw, value.Raw) {
			return ErrNonceConflict
		}
	case state == "confirmed":
		if existing.Nonce == ^uint64(0) || value.Nonce != existing.Nonce+1 {
			return ErrNonceConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE registration_transactions SET file_id=?,target=?,nonce=?,tx_hash=?,raw_tx=?,state='prepared' WHERE chain_id=? AND account=?`, value.FileID, value.Target, strconv.FormatUint(value.Nonce, 10), value.Hash, value.Raw, value.ChainID, value.Account)
	default:
		return errors.New("invalid transaction journal state")
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// CompletePreparedRegistration is called only after the registration layer has
// checked confirmations, canonical receipt, and matching confirmed readback.
// Keep the last confirmed nonce rather than freeing a lane to reuse old nonces.
func (s *Store) CompletePreparedRegistration(ctx context.Context, job RegistrationJob, value PreparedRegistration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE registration_jobs SET status='registered',lease_token='',lease_until=0,last_error='' WHERE file_id=? AND target=? AND lease_token=? AND lease_until>? AND tx_hash=?`, job.FileID, job.Target, job.Token, time.Now().UnixMilli(), value.Hash)
	if err != nil {
		return err
	}
	if err := checkLeaseResult(result); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `UPDATE registration_transactions SET state='confirmed' WHERE chain_id=? AND account=? AND file_id=? AND target=? AND tx_hash=? AND state='prepared'`, value.ChainID, value.Account, job.FileID, job.Target, value.Hash)
	if err != nil {
		return err
	}
	if err := checkLeaseResult(result); err != nil {
		return err
	}
	return tx.Commit()
}
