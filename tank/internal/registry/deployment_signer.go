package registry

import (
	"context"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
)

// SignDeployment is deliberately distinct from the registration signer: only
// Arc testnet contract creation with zero value is allowed. DeployArc enforces operator fee and gas bounds.
func (s *KeystoreSigner) SignDeployment(ctx context.Context, tx *types.Transaction) (*types.Transaction, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if tx == nil || s.key.D.Sign() == 0 || tx.To() != nil || tx.Value().Sign() != 0 || tx.Type() != types.DynamicFeeTxType || tx.ChainId().Cmp(big.NewInt(ArcTestnetChainID)) != 0 || len(tx.Data()) == 0 || tx.Gas() < 21000 || tx.Gas() > 30_000_000 || len(tx.AccessList()) != 0 || tx.GasFeeCap().Cmp(big.NewInt(ArcMinimumFee)) < 0 || tx.GasTipCap().Sign() < 0 || tx.GasTipCap().Cmp(tx.GasFeeCap()) > 0 {
		return nil, ErrCustody
	}
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(ArcTestnetChainID)), s.key)
	if err != nil {
		return nil, ErrCustody
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return signed, nil
}
