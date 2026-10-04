package registry

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/metadata"
)

const contractABI = `[
  {
    "type":"function",
    "name":"tank",
    "stateMutability":"nonpayable",
    "inputs":[
      {"name":"fileId","type":"bytes32"},
      {"name":"commitment","type":"bytes32"},
      {"name":"fileSize","type":"uint64"}
    ],
    "outputs":[]
  },
  {
    "type":"function",
    "name":"getTank",
    "stateMutability":"view",
    "inputs":[
      {"name":"registrant","type":"address"},
      {"name":"fileId","type":"bytes32"}
    ],
    "outputs":[{
      "name":"",
      "type":"tuple",
      "components":[
        {"name":"commitment","type":"bytes32"},
        {"name":"fileSize","type":"uint64"},
        {"name":"registeredAt","type":"uint64"}
      ]
    }]
  }
]`

type Config struct {
	RPCURL     string
	Contract   string
	Registrant string
}

type Record struct {
	Commitment   integrity.Hash
	FileSize     uint64
	RegisteredAt uint64
}

type Client struct {
	rpc        *rpc.Client
	chain      *ethclient.Client
	abi        abi.ABI
	contract   common.Address
	registrant common.Address
}

func Open(ctx context.Context, cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.RPCURL)
	if err != nil || u.Host == "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid local RPC URL")
	}

	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("unlocked-account client requires a loopback RPC")
	}

	if !common.IsHexAddress(cfg.Contract) ||
		!common.IsHexAddress(cfg.Registrant) {
		return nil, fmt.Errorf("invalid contract or registrant address")
	}

	contract := common.HexToAddress(cfg.Contract)
	registrant := common.HexToAddress(cfg.Registrant)
	if contract == (common.Address{}) || registrant == (common.Address{}) {
		return nil, fmt.Errorf("addresses cannot be zero")
	}

	parsed, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return nil, err
	}

	connection, err := rpc.DialContext(ctx, cfg.RPCURL)
	if err != nil {
		return nil, err
	}
	chain := ethclient.NewClient(connection)

	fail := func(err error) (*Client, error) {
		connection.Close()
		return nil, err
	}

	chainID, err := chain.ChainID(ctx)
	if err != nil {
		return fail(err)
	}
	if chainID.String() != "31337" {
		return fail(fmt.Errorf("expected local chain 31337, got %s", chainID))
	}

	code, err := chain.CodeAt(ctx, contract, nil)
	if err != nil {
		return fail(err)
	}
	if len(code) == 0 {
		return fail(fmt.Errorf("no contract exists at %s", contract.Hex()))
	}

	return &Client{
		rpc:        connection,
		chain:      chain,
		abi:        parsed,
		contract:   contract,
		registrant: registrant,
	}, nil
}

func (c *Client) Close() {
	c.rpc.Close()
}

func (c *Client) Get(
	ctx context.Context,
	id integrity.Hash,
) (Record, bool, error) {
	data, err := c.abi.Pack(
		"getTank", c.registrant, [32]byte(id),
	)
	if err != nil {
		return Record{}, false, err
	}

	result, err := c.chain.CallContract(ctx, ethereum.CallMsg{
		To: &c.contract, Data: data,
	}, nil)
	if err != nil {
		var dataError rpc.DataError
		if errors.As(err, &dataError) {
			revert, ok := dataError.ErrorData().(string)
			selector := crypto.Keccak256([]byte("RecordNotFound()"))[:4]
			if ok && strings.EqualFold(revert, hexutil.Encode(selector)) {
				return Record{}, false, nil
			}
		}
		return Record{}, false, err
	}

	record, err := decodeRecord(result)
	return record, err == nil, err
}

func decodeRecord(data []byte) (Record, error) {
	var record Record

	// Three static ABI words, with canonical uint64 padding.
	if len(data) != 96 ||
		!bytes.Equal(data[32:56], make([]byte, 24)) ||
		!bytes.Equal(data[64:88], make([]byte, 24)) {
		return record, fmt.Errorf("invalid registry response")
	}

	copy(record.Commitment[:], data[:32])
	record.FileSize = binary.BigEndian.Uint64(data[56:64])
	record.RegisteredAt = binary.BigEndian.Uint64(data[88:96])

	if record.Commitment == (integrity.Hash{}) || record.FileSize == 0 {
		return Record{}, fmt.Errorf("invalid registry record")
	}

	return record, nil
}

// Register returns a zero transaction hash if the same record already exists.
func (c *Client) Register(
	ctx context.Context,
	m metadata.Manifest,
) (common.Hash, error) {
	commitment, err := m.Commitment()
	if err != nil {
		return common.Hash{}, err
	}
	id, err := integrity.ParseHash(m.FileID)
	if err != nil {
		return common.Hash{}, err
	}

	existing, found, err := c.Get(ctx, id)
	if err != nil {
		return common.Hash{}, err
	}
	if found {
		if existing.Commitment != commitment ||
			existing.FileSize != uint64(m.Size) {
			return common.Hash{}, fmt.Errorf("existing registry commitment conflicts")
		}
		return common.Hash{}, nil
	}

	data, err := c.abi.Pack(
		"tank", [32]byte(id), [32]byte(commitment), uint64(m.Size),
	)
	if err != nil {
		return common.Hash{}, err
	}

	gas, err := c.chain.EstimateGas(ctx, ethereum.CallMsg{
		From: c.registrant, To: &c.contract, Data: data,
	})
	if err != nil {
		return common.Hash{}, err
	}

	var transaction common.Hash
	err = c.rpc.CallContext(ctx, &transaction, "eth_sendTransaction",
		map[string]any{
			"from": c.registrant.Hex(),
			"to":   c.contract.Hex(),
			"data": hexutil.Encode(data),
			"gas":  hexutil.Uint64(gas),
		},
	)

	return transaction, err
}

func (c *Client) Wait(ctx context.Context, transaction common.Hash) error {
	if transaction == (common.Hash{}) {
		return nil
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		receipt, err := c.chain.TransactionReceipt(ctx, transaction)
		if err == nil {
			if receipt.Status != types.ReceiptStatusSuccessful {
				return fmt.Errorf("registration transaction reverted")
			}
			return nil
		}
		if !errors.Is(err, ethereum.NotFound) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
