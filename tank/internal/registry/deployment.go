package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var ErrDeployment = errors.New("Arc testnet deployment prerequisites or verification failed")

type DeploymentConfig struct {
	ChainID                 int64
	RPCURL                  string
	Sender                  common.Address
	Creation, Runtime       []byte
	MaxGas                  uint64
	MaxFee, MaxTip, MaxCost *big.Int
	Journal                 string
}
type DeploymentResult struct {
	ChainID     int64  `json:"chain_id"`
	Sender      string `json:"sender"`
	Address     string `json:"address"`
	Transaction string `json:"transaction"`
	Explorer    string `json:"explorer"`
	MaximumCost string `json:"maximum_cost_native_units"`
	Deployed    bool   `json:"deployed"`
}

func (c DeploymentConfig) valid() bool {
	u, e := url.Parse(c.RPCURL)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && c.ChainID == ArcTestnetChainID && c.Sender != (common.Address{}) && len(c.Creation) > 0 && len(c.Runtime) > 0 && c.MaxGas >= 21000 && c.MaxGas <= 30_000_000 && positive256(c.MaxFee) && c.MaxTip != nil && c.MaxTip.Sign() >= 0 && c.MaxTip.Cmp(c.MaxFee) <= 0 && positive256(c.MaxCost)
}
func deploymentNetwork(ctx context.Context, client *ethclient.Client) error {
	id, e := client.ChainID(ctx)
	if e != nil || id == nil || id.Cmp(big.NewInt(ArcTestnetChainID)) != 0 {
		return ErrDeployment
	}
	return nil
}

// DeployArc performs bounded preflight or resumes precisely the privately
// journaled transaction. A journal never permits a second deployment nonce.
func DeployArc(ctx context.Context, c DeploymentConfig, s *KeystoreSigner, broadcast bool) (DeploymentResult, error) {
	return deployArc(ctx, c, s, broadcast, &http.Client{})
}
func deployArc(ctx context.Context, c DeploymentConfig, s *KeystoreSigner, broadcast bool, httpClient *http.Client) (DeploymentResult, error) {
	var out DeploymentResult
	if !c.valid() || s == nil || s.Address() != c.Sender {
		return out, ErrDeployment
	}
	clientHTTP := *httpClient
	clientHTTP.Timeout = 10 * time.Second
	clientHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	base := clientHTTP.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	clientHTTP.Transport = boundedRPCTransport{base: base}
	connection, e := rpc.DialOptions(ctx, c.RPCURL, rpc.WithHTTPClient(&clientHTTP))
	if e != nil {
		return out, ErrDeployment
	}
	defer connection.Close()
	client := ethclient.NewClient(connection)
	if e = deploymentNetwork(ctx, client); e != nil {
		return out, e
	}
	out = DeploymentResult{ChainID: c.ChainID, Sender: c.Sender.Hex()}
	var tx types.Transaction
	var raw []byte
	lock, err := deploymentLock(c.Journal)
	if err != nil {
		return out, err
	}
	defer lock.Close()
	if broadcast {
		raw, e = privateFile(c.Journal, 1<<20)
		if e != nil {
			if _, staterr := os.Lstat(c.Journal); !os.IsNotExist(staterr) {
				return out, ErrDeployment
			}
		} else if tx.UnmarshalBinary(raw) != nil {
			return out, ErrDeployment
		}
	}
	if len(raw) == 0 {
		fee, e := client.SuggestGasPrice(ctx)
		if e != nil || !positive256(fee) {
			return out, ErrDeployment
		}
		fee = new(big.Int).Mul(fee, big.NewInt(2))
		if fee.Cmp(big.NewInt(ArcMinimumFee)) < 0 {
			fee.SetInt64(ArcMinimumFee)
		}
		if fee.Cmp(c.MaxFee) > 0 || c.MaxTip.Cmp(fee) > 0 {
			return out, ErrDeployment
		}
		gas, e := client.EstimateGas(ctx, ethereum.CallMsg{From: c.Sender, Data: c.Creation, GasFeeCap: fee, GasTipCap: c.MaxTip, Value: new(big.Int)})
		if e != nil || gas < 21000 || gas > c.MaxGas || gas > c.MaxGas*5/6 {
			return out, ErrDeployment
		}
		gas += gas / 5
		cost := new(big.Int).Mul(new(big.Int).SetUint64(gas), fee)
		balance, e := client.BalanceAt(ctx, c.Sender, nil)
		if e != nil || balance == nil || balance.Cmp(cost) < 0 || cost.Cmp(c.MaxCost) > 0 {
			return out, ErrDeployment
		}
		out.MaximumCost = cost.String()
		if !broadcast {
			return out, nil
		}
		nonce, e := client.PendingNonceAt(ctx, c.Sender)
		if e != nil {
			return out, ErrDeployment
		}
		if e = deploymentNetwork(ctx, client); e != nil {
			return out, e
		}
		unsigned := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(c.ChainID), Nonce: nonce, Gas: gas, GasFeeCap: fee, GasTipCap: c.MaxTip, Value: new(big.Int), Data: c.Creation})
		signed, e := s.SignDeployment(ctx, unsigned)
		if e != nil {
			return out, e
		}
		raw, e = signed.MarshalBinary()
		if e != nil {
			return out, ErrDeployment
		}
		if e = writeDeploymentJournal(c.Journal, raw); e != nil {
			return out, e
		}
		if e = tx.UnmarshalBinary(raw); e != nil {
			return out, ErrDeployment
		}
	}
	sender, e := types.Sender(types.LatestSignerForChainID(big.NewInt(c.ChainID)), &tx)
	if e != nil || sender != c.Sender || tx.ChainId().Cmp(big.NewInt(c.ChainID)) != 0 || tx.Type() != types.DynamicFeeTxType || tx.To() != nil || tx.Value().Sign() != 0 || !bytes.Equal(tx.Data(), c.Creation) || tx.Gas() < 21000 || tx.Gas() > c.MaxGas || len(tx.AccessList()) != 0 || tx.GasFeeCap().Cmp(big.NewInt(ArcMinimumFee)) < 0 || tx.GasTipCap().Cmp(tx.GasFeeCap()) > 0 || tx.GasFeeCap().Cmp(c.MaxFee) > 0 || tx.GasTipCap().Cmp(c.MaxTip) > 0 || new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap()).Cmp(c.MaxCost) > 0 {
		return out, ErrDeployment
	}
	out.MaximumCost = new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap()).String()
	address := crypto.CreateAddress(c.Sender, tx.Nonce())
	out.Address = address.Hex()
	out.Transaction = tx.Hash().Hex()
	out.Explorer = "https://explorer.testnet.arc.io/tx/" + tx.Hash().Hex()
	if e = deploymentNetwork(ctx, client); e != nil {
		return out, e
	}
	receipt, e := client.TransactionReceipt(ctx, tx.Hash())
	if errors.Is(e, ethereum.NotFound) {
		balance, err := client.BalanceAt(ctx, c.Sender, nil)
		cost := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap())
		if err != nil || balance == nil || balance.Cmp(cost) < 0 {
			return out, ErrDeployment
		}
		if err = deploymentNetwork(ctx, client); err != nil {
			return out, err
		}
		_ = client.SendTransaction(ctx, &tx)
	} else if e != nil {
		return out, ErrDeployment
	}
	for {
		receipt, e = client.TransactionReceipt(ctx, tx.Hash())
		if e == nil {
			break
		}
		if !errors.Is(e, ethereum.NotFound) {
			return out, ErrDeployment
		}
		select {
		case <-ctx.Done():
			return out, ErrDeployment
		case <-time.After(time.Second):
		}
	}
	if receipt.Status != 1 || receipt.TxHash != tx.Hash() || receipt.ContractAddress != address || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() {
		return out, ErrDeployment
	}
	header, e := client.HeaderByNumber(ctx, receipt.BlockNumber)
	if e != nil || header == nil || header.Number == nil || header.Number.Cmp(receipt.BlockNumber) != 0 || header.Hash() != receipt.BlockHash {
		return out, ErrDeployment
	}
	code, e := client.CodeAtHash(ctx, address, receipt.BlockHash)
	if e != nil || !bytes.Equal(code, c.Runtime) {
		return out, ErrDeployment
	}
	header, e = client.HeaderByNumber(ctx, receipt.BlockNumber)
	if e != nil || header == nil || header.Number == nil || header.Number.Cmp(receipt.BlockNumber) != 0 || header.Hash() != receipt.BlockHash {
		return out, ErrDeployment
	}
	if e = deploymentNetwork(ctx, client); e != nil {
		return out, e
	}
	out.Deployed = true
	return out, nil
}
func deploymentLock(path string) (*os.File, error) {
	info, e := os.Lstat(filepath.Dir(path))
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrDeployment
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return nil, ErrDeployment
	}
	fd, e := syscall.Open(path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, ErrDeployment
	}
	f := os.NewFile(uintptr(fd), "deployment lock")
	info, e = f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, ErrDeployment
	}
	st, ok = info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		f.Close()
		return nil, ErrDeployment
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, ErrDeployment
	}
	return f, nil
}
func writeDeploymentJournal(path string, raw []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".deployment-")
	if e != nil {
		return ErrDeployment
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return ErrDeployment
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return ErrDeployment
	}
	if e = f.Close(); e != nil {
		return ErrDeployment
	}
	if e = os.Link(name, path); e != nil {
		return ErrDeployment
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return ErrDeployment
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrDeployment
	}
	return nil
}

// LoadDeploymentArtifact uses Forge's compiled creation and runtime bytecode.
func LoadDeploymentArtifact(path string) ([]byte, []byte, error) {
	// A context deadline cannot interrupt a blocking FIFO open. Check the actual
	// descriptor before reading; ordinary compiled-artifact symlinks remain valid.
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, nil, ErrDeployment
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 4<<20 {
		return nil, nil, ErrDeployment
	}
	data, e := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if e != nil || len(data) > 4<<20 {
		return nil, nil, ErrDeployment
	}
	var a struct {
		Bytecode struct {
			Object string `json:"object"`
		} `json:"bytecode"`
		Deployed struct {
			Object string `json:"object"`
		} `json:"deployedBytecode"`
	}
	if json.Unmarshal(data, &a) != nil {
		return nil, nil, ErrDeployment
	}
	creation, e := hexutil.Decode(a.Bytecode.Object)
	if e != nil {
		return nil, nil, ErrDeployment
	}
	runtime, e := hexutil.Decode(a.Deployed.Object)
	if e != nil || len(creation) == 0 || len(runtime) == 0 {
		return nil, nil, ErrDeployment
	}
	return creation, runtime, nil
}
