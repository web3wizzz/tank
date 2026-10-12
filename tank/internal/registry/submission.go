package registry

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

var ErrSubmission = errors.New("authorized Arc Testnet submission unavailable or invalid")

// Submit sends a signed immutable intent, never an unlocked-account request.
// Provider failures are deliberately redacted, including ambiguous acceptance.
func (r *TestnetReader) Submit(ctx context.Context, transaction *types.Transaction, p TestnetPolicy) error {
	if !p.valid() || p.ChainID != ArcTestnetChainID || r.chainID != ArcTestnetChainID || p.Contract != r.contract || p.Registrant != r.registrant || !p.allows(transaction) {
		return ErrSubmission
	}
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(ArcTestnetChainID)), transaction)
	if err != nil || sender != p.Registrant {
		return ErrSubmission
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.verifyNetwork(ctx); err != nil {
		return ErrSubmission
	}
	if err := r.chain.SendTransaction(ctx, transaction); err != nil {
		return ErrSubmission
	}
	return nil
}
