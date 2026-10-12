package metadata

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/core/types"
)

var ErrSpendBudget = errors.New("registration aggregate spend budget unavailable or exhausted")

// ReserveRegistrationSpend permanently charges the signed transaction's maximum
// execution cost before any broadcast. Retries use the same reservation, even
// after restart. Confirmed costs are not refunded; this deliberately conservative
// budget covers this journal/account only. The dedicated account must not be used
// by other journals, tools or applications.
func (s *Store) ReserveRegistrationSpend(ctx context.Context, job RegistrationJob, value PreparedRegistration, budget *big.Int) error {
	if budget == nil || budget.Sign() <= 0 || budget.BitLen() > 256 || value.ChainID != 5042002 {
		return ErrSpendBudget
	}
	var signed types.Transaction
	if signed.UnmarshalBinary(value.Raw) != nil || signed.Type() != types.DynamicFeeTxType || signed.ChainId().Cmp(big.NewInt(value.ChainID)) != 0 || signed.GasFeeCap().Sign() <= 0 || signed.Hash().Hex() != value.Hash {
		// Hash() hex uses lowercase numeric hex, including its 0x prefix.
		return ErrSpendBudget
	}
	account, err := types.Sender(types.LatestSignerForChainID(signed.ChainId()), &signed)
	if err != nil || strings.ToLower(account.Hex()) != value.Account {
		return ErrSpendBudget
	}
	cost := new(big.Int).Mul(new(big.Int).SetUint64(signed.Gas()), signed.GasFeeCap())
	if cost.Sign() <= 0 || cost.Cmp(budget) > 0 {
		return ErrSpendBudget
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSpendBudget
	}
	defer tx.Rollback()
	// Acquire SQLite's write lock before reading/changing the shared counter.
	if _, err := tx.ExecContext(ctx, `INSERT INTO registration_spend_accounts(chain_id,account,budget,reserved) VALUES(?,?,?,'0') ON CONFLICT(chain_id,account) DO NOTHING`, value.ChainID, value.Account, budget.String()); err != nil {
		return ErrSpendBudget
	}
	if err := registrationLease(ctx, tx, job); err != nil {
		return err
	}
	existing, state, err := scanPrepared(tx.QueryRowContext(ctx, `SELECT chain_id,account,file_id,target,nonce,tx_hash,raw_tx,state FROM registration_transactions WHERE chain_id=? AND account=?`, value.ChainID, value.Account))
	if err != nil || state != "prepared" || existing.FileID != job.FileID || existing.Target != job.Target || existing.Hash != value.Hash || !bytes.Equal(existing.Raw, value.Raw) {
		return ErrSpendBudget
	}
	var limitText, totalText string
	if err := tx.QueryRowContext(ctx, `SELECT budget,reserved FROM registration_spend_accounts WHERE chain_id=? AND account=?`, value.ChainID, value.Account).Scan(&limitText, &totalText); err != nil {
		return ErrSpendBudget
	}
	if limitText != budget.String() {
		return ErrSpendBudget
	}
	total, ok := new(big.Int).SetString(totalText, 10)
	if !ok || total.Sign() < 0 || total.Cmp(budget) > 0 {
		return ErrSpendBudget
	}
	var reservedText string
	err = tx.QueryRowContext(ctx, `SELECT cost FROM registration_spend_reservations WHERE chain_id=? AND account=? AND tx_hash=?`, value.ChainID, value.Account, value.Hash).Scan(&reservedText)
	if err == nil {
		if reservedText != cost.String() {
			return ErrSpendBudget
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ErrSpendBudget
	}
	total.Add(total, cost)
	if total.Cmp(budget) > 0 {
		return ErrSpendBudget
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO registration_spend_reservations(chain_id,account,tx_hash,cost) VALUES(?,?,?,?)`, value.ChainID, value.Account, value.Hash, cost.String()); err != nil {
		return ErrSpendBudget
	}
	if _, err := tx.ExecContext(ctx, `UPDATE registration_spend_accounts SET reserved=? WHERE chain_id=? AND account=?`, total.String(), value.ChainID, value.Account); err != nil {
		return ErrSpendBudget
	}
	if err := tx.Commit(); err != nil {
		return ErrSpendBudget
	}
	return nil
}
