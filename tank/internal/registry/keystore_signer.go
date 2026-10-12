package registry

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"io"
	"math/big"
	"os"
	"strings"
	"syscall"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var ErrCustody = errors.New("private signing configuration unavailable or invalid")

// KeystoreSigner is an offline-only adapter. It holds no RPC transport and
// accepts an encrypted V3 keystore and password file, never a raw key argument.
type KeystoreSigner struct {
	key     *ecdsa.PrivateKey
	address common.Address
}

func privateFile(path string, maximum int64) ([]byte, error) {
	// Refuse symlinks and inspect the opened descriptor, avoiding a path-check
	// race. Configuration must live in an operator-owned private directory.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrCustody
	}
	f := os.NewFile(uintptr(fd), "private signing file")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() <= 0 || info.Size() > maximum {
		return nil, ErrCustody
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, ErrCustody
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, ErrCustody
	}
	return data, nil
}

func OpenKeystoreSigner(ctx context.Context, keyPath, passwordPath string, expected common.Address) (*KeystoreSigner, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if expected == (common.Address{}) {
		return nil, ErrCustody
	}
	encoded, err := privateFile(keyPath, 1<<20)
	if err != nil {
		return nil, ErrCustody
	}
	password, err := privateFile(passwordPath, 4096)
	if err != nil {
		return nil, ErrCustody
	}
	defer clear(password)
	key, err := keystore.DecryptKey(encoded, strings.TrimSuffix(strings.TrimSuffix(string(password), "\n"), "\r"))
	if err != nil {
		return nil, ErrCustody
	}
	if ctx.Err() != nil {
		key.PrivateKey.D.SetInt64(0)
		return nil, ctx.Err()
	}
	address := crypto.PubkeyToAddress(key.PrivateKey.PublicKey)
	// Refuse the ubiquitous first Anvil account. Operators must supply a
	// dedicated account; test-only generated accounts never enter configuration.
	if address != expected || address == common.HexToAddress("0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266") {
		key.PrivateKey.D.SetInt64(0)
		return nil, ErrCustody
	}
	return &KeystoreSigner{key: key.PrivateKey, address: address}, nil
}

func (s *KeystoreSigner) Address() common.Address { return s.address }

// Close is called after the worker stops. Do not race it with signing.
func (s *KeystoreSigner) Close() { s.key.D.SetInt64(0) }
func (s *KeystoreSigner) SignTransaction(ctx context.Context, tx *types.Transaction, chain *big.Int) (*types.Transaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.key.D.Sign() == 0 || tx == nil || chain == nil || chain.Cmp(big.NewInt(ArcTestnetChainID)) != 0 || tx.ChainId().Cmp(chain) != 0 || tx.Type() != types.DynamicFeeTxType || tx.Value().Sign() != 0 || tx.To() == nil {
		return nil, ErrCustody
	}
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(chain), s.key)
	if err != nil {
		return nil, ErrCustody
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return signed, nil
}
