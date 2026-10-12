package registry

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
	"time"
)

var ErrTestnetRPC = errors.New("testnet read-only RPC unavailable or invalid")

// TestnetReader exposes only read methods. It has no signer or submission API.
// No service constructs it until an operator selects a provider and registry.
type TestnetReader struct {
	connection           *rpc.Client
	chain                *ethclient.Client
	contract, registrant common.Address
	abi                  abi.ABI
	chainID              int64
}

type boundedRPCTransport struct{ base http.RoundTripper }
type rpcBody struct {
	io.Reader
	io.Closer
}

func (transport boundedRPCTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > 1<<20 {
		response.Body.Close()
		return nil, ErrTestnetRPC
	}
	response.Body = rpcBody{Reader: io.LimitReader(response.Body, 1<<20), Closer: response.Body}
	return response, nil
}

func OpenTestnetReader(ctx context.Context, cfg Config) (*TestnetReader, error) {
	return openTestnetReader(ctx, cfg, &http.Client{})
}
func openTestnetReader(ctx context.Context, cfg Config, httpClient *http.Client) (*TestnetReader, error) {
	if cfg.ChainID != ArcTestnetChainID {
		return nil, ErrTestnetRPC
	}
	u, err := url.Parse(cfg.RPCURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || !common.IsHexAddress(cfg.Contract) || !common.IsHexAddress(cfg.Registrant) || common.HexToAddress(cfg.Contract) == (common.Address{}) || common.HexToAddress(cfg.Registrant) == (common.Address{}) {
		return nil, ErrTestnetRPC
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := *httpClient
	client.Timeout = 10 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = boundedRPCTransport{base: transport}
	connection, err := rpc.DialOptions(ctx, cfg.RPCURL, rpc.WithHTTPClient(&client))
	if err != nil {
		return nil, ErrTestnetRPC
	}
	parsed, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		connection.Close()
		return nil, ErrTestnetRPC
	}
	reader := &TestnetReader{chainID: cfg.ChainID, connection: connection, chain: ethclient.NewClient(connection), contract: common.HexToAddress(cfg.Contract), registrant: common.HexToAddress(cfg.Registrant), abi: parsed}
	if err := reader.verifyNetwork(ctx); err != nil {
		reader.Close()
		return nil, err
	}
	code, err := reader.chain.CodeAt(ctx, reader.contract, nil)
	if err != nil || len(code) == 0 {
		reader.Close()
		return nil, ErrTestnetRPC
	}
	return reader, nil
}
func (r *TestnetReader) Close() { r.connection.Close() }
func (r *TestnetReader) verifyNetwork(ctx context.Context) error {
	chain, err := r.chain.ChainID(ctx)
	if err != nil || chain.Cmp(big.NewInt(r.chainID)) != 0 {
		return ErrTestnetRPC
	}
	return nil
}
func (r *TestnetReader) PendingNonce(ctx context.Context) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.verifyNetwork(ctx); err != nil {
		return 0, err
	}
	nonce, err := r.chain.PendingNonceAt(ctx, r.registrant)
	if err != nil {
		return 0, ErrTestnetRPC
	}
	return nonce, nil
}

// Terms uses native 18-decimal USDC units throughout. Estimates never raise an
// operator bound. An unavailable estimate/balance fails closed before signing.
func (r *TestnetReader) Terms(ctx context.Context, m metadata.Manifest, p TestnetPolicy) (TransactionTerms, error) {
	var empty TransactionTerms
	if !p.valid() || p.ChainID != r.chainID || p.Contract != r.contract || p.Registrant != r.registrant {
		return empty, ErrPreparation
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.verifyNetwork(ctx); err != nil {
		return empty, err
	}
	data, err := registrationData(m)
	if err != nil {
		return empty, err
	}
	fee, err := r.chain.SuggestGasPrice(ctx)
	if err != nil || !positive256(fee) {
		return empty, ErrTestnetRPC
	}
	fee = new(big.Int).Mul(fee, big.NewInt(2))
	if fee.Cmp(big.NewInt(ArcMinimumFee)) < 0 {
		fee.SetInt64(ArcMinimumFee)
	}
	if fee.Cmp(p.MaxFeeWei) > 0 {
		return empty, ErrPreparation
	}
	tip := new(big.Int).Set(p.MaxTipWei)
	gas, err := r.chain.EstimateGas(ctx, ethereum.CallMsg{From: r.registrant, To: &r.contract, Value: new(big.Int), Data: data, GasFeeCap: fee, GasTipCap: tip})
	if err != nil || gas < 21000 || gas > p.MaxGas || gas > (p.MaxGas*5)/6 {
		return empty, ErrPreparation
	}
	gas += gas / 5 // 20% simulation margin, still within explicit ceiling
	cost := new(big.Int).Mul(new(big.Int).SetUint64(gas), fee)
	if cost.Cmp(p.MaxExecutionCostWei) > 0 {
		return empty, ErrPreparation
	}
	balance, err := r.chain.BalanceAt(ctx, r.registrant, nil)
	if err != nil || balance == nil || balance.Cmp(cost) < 0 {
		return empty, ErrPreparation
	}
	nonce, err := r.chain.PendingNonceAt(ctx, r.registrant)
	if err != nil {
		return empty, ErrTestnetRPC
	}
	if err := r.verifyNetwork(ctx); err != nil {
		return empty, err
	}
	return TransactionTerms{Nonce: nonce, Gas: gas, FeeWei: fee, TipWei: tip}, nil
}

// Observation requests canonical, hash-pinned readback and rechecks that the
// receipt block stayed canonical while reading. ConfirmPreparedRegistration
// additionally verifies depth/commitment/size under current journal/job fencing.
func (r *TestnetReader) Observation(ctx context.Context, transaction common.Hash, id integrity.Hash) (ConfirmedObservation, error) {
	var empty ConfirmedObservation
	if transaction == (common.Hash{}) || id == (integrity.Hash{}) {
		return empty, ErrPreparation
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.verifyNetwork(ctx); err != nil {
		return empty, err
	}
	receipt, err := r.chain.TransactionReceipt(ctx, transaction)
	if errors.Is(err, ethereum.NotFound) {
		return empty, ErrConfirmation
	}
	if err != nil {
		return empty, ErrTestnetRPC
	}
	if receipt.Status != 1 || receipt.TxHash != transaction || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() {
		return empty, ErrConfirmation
	}
	header, err := r.chain.HeaderByNumber(ctx, receipt.BlockNumber)
	if err != nil {
		return empty, ErrTestnetRPC
	}
	if header.Number == nil || header.Number.Cmp(receipt.BlockNumber) != 0 || header.Hash() != receipt.BlockHash {
		return empty, ErrConfirmation
	}
	data, err := r.abi.Pack("getTank", r.registrant, [32]byte(id))
	if err != nil {
		return empty, ErrPreparation
	}
	var encoded hexutil.Bytes
	err = r.connection.CallContext(ctx, &encoded, "eth_call", map[string]any{"to": r.contract.Hex(), "from": r.registrant.Hex(), "data": hexutil.Encode(data)}, rpc.BlockNumberOrHashWithHash(receipt.BlockHash, true))
	if err != nil {
		return empty, ErrTestnetRPC
	}
	record, err := decodeRecord(encoded)
	if err != nil {
		return empty, ErrConfirmation
	}
	head, err := r.chain.BlockNumber(ctx)
	if err != nil {
		return empty, ErrTestnetRPC
	}
	canonical, err := r.chain.HeaderByNumber(ctx, receipt.BlockNumber)
	if err != nil {
		return empty, ErrTestnetRPC
	}
	if canonical.Number == nil || canonical.Number.Cmp(receipt.BlockNumber) != 0 || canonical.Hash() != receipt.BlockHash {
		return empty, ErrConfirmation
	}
	return ConfirmedObservation{Receipt: receipt, CanonicalHeader: canonical, Head: head, ReadbackBlockHash: receipt.BlockHash, Record: record}, nil
}
