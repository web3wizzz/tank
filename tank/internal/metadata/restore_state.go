package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
)

var ErrRestoreState = errors.New("restore durable state unavailable or inconsistent")

type RestoreJob struct {
	FileID, Target, Status, Hash string
	Manifest                     Manifest
}
type RestoreLane struct {
	PreparedRegistration
	State string
}
type RestoreAccount struct {
	ChainID                   int64
	Account, Budget, Reserved string
}
type RestoreReservation struct {
	ChainID             int64
	Account, Hash, Cost string
}
type RestoreState struct {
	Jobs         []RestoreJob
	Lanes        []RestoreLane
	Accounts     []RestoreAccount
	Reservations []RestoreReservation
}

// OpenRestoreReadOnly never creates a database or applies schema migrations.
func OpenRestoreReadOnly(ctx context.Context, path string) (*Store, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrRestoreState
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrRestoreState
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}).String())
	if err != nil {
		return nil, ErrRestoreState
	}
	db.SetMaxOpenConns(1)
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 8 {
		db.Close()
		return nil, ErrRestoreState
	}
	return &Store{db: db}, nil
}

// RestoreSnapshot captures all registration state in one read transaction.
func (s *Store) RestoreSnapshot(ctx context.Context) (state RestoreState, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return state, ErrRestoreState
	}
	defer tx.Rollback()
	read := func(query string, scan func(*sql.Rows) error) error {
		rows, e := tx.QueryContext(ctx, query)
		if e != nil {
			return ErrRestoreState
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
			if count > 100000 || scan(rows) != nil {
				return ErrRestoreState
			}
		}
		if rows.Err() != nil {
			return ErrRestoreState
		}
		return nil
	}
	if err = read(`SELECT j.file_id,j.target,j.status,j.tx_hash,m.body FROM registration_jobs j LEFT JOIN manifests m ON m.file_id=j.file_id ORDER BY j.file_id,j.target`, func(r *sql.Rows) error {
		var j RestoreJob
		var body sql.NullString
		if e := r.Scan(&j.FileID, &j.Target, &j.Status, &j.Hash, &body); e != nil {
			return e
		}
		if !body.Valid || json.Unmarshal([]byte(body.String), &j.Manifest) != nil || j.Manifest.Validate() != nil || j.Manifest.FileID != j.FileID {
			return ErrRestoreState
		}
		state.Jobs = append(state.Jobs, j)
		return nil
	}); err != nil {
		return RestoreState{}, err
	}
	if err = read(`SELECT chain_id,account,file_id,target,nonce,tx_hash,raw_tx,state FROM registration_transactions ORDER BY chain_id,account`, func(r *sql.Rows) error {
		v, st, e := scanPrepared(r)
		if e != nil {
			return e
		}
		state.Lanes = append(state.Lanes, RestoreLane{v, st})
		return nil
	}); err != nil {
		return RestoreState{}, err
	}
	if err = read(`SELECT chain_id,account,budget,reserved FROM registration_spend_accounts ORDER BY chain_id,account`, func(r *sql.Rows) error {
		var v RestoreAccount
		if e := r.Scan(&v.ChainID, &v.Account, &v.Budget, &v.Reserved); e != nil {
			return e
		}
		state.Accounts = append(state.Accounts, v)
		return nil
	}); err != nil {
		return RestoreState{}, err
	}
	if err = read(`SELECT chain_id,account,tx_hash,cost FROM registration_spend_reservations ORDER BY chain_id,account,tx_hash`, func(r *sql.Rows) error {
		var v RestoreReservation
		if e := r.Scan(&v.ChainID, &v.Account, &v.Hash, &v.Cost); e != nil {
			return e
		}
		state.Reservations = append(state.Reservations, v)
		return nil
	}); err != nil {
		return RestoreState{}, err
	}
	if tx.Commit() != nil {
		return RestoreState{}, ErrRestoreState
	}
	return state, nil
}
