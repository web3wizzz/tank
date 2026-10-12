package registry

import (
	"context"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentRejectsOtherNetworks(t *testing.T) {
	key, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	s := &KeystoreSigner{key: key, address: crypto.PubkeyToAddress(key.PublicKey)}
	defer s.Close()
	for _, id := range []int64{5042, 1, 5042001, 31337, 84532, 0} {
		c := DeploymentConfig{ChainID: id, RPCURL: "https://unreachable.invalid", Sender: s.Address(), Creation: []byte{1}, Runtime: []byte{2}, MaxGas: 100000, MaxFee: big.NewInt(ArcMinimumFee), MaxTip: big.NewInt(0), MaxCost: big.NewInt(10000000000000000)}
		if _, e := DeployArc(context.Background(), c, s, true); e != ErrDeployment {
			t.Fatalf("network %d accepted", id)
		}
		tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(id), Gas: 100000, GasFeeCap: big.NewInt(ArcMinimumFee), GasTipCap: big.NewInt(0), Value: big.NewInt(0), Data: []byte{1}})
		if _, e := s.SignDeployment(context.Background(), tx); e != ErrCustody {
			t.Fatalf("network %d signed", id)
		}
	}
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(ArcTestnetChainID), Gas: 100000, GasFeeCap: big.NewInt(ArcMinimumFee), GasTipCap: big.NewInt(0), Value: big.NewInt(0), Data: []byte{1}})
	if _, e := s.SignTransaction(context.Background(), tx, big.NewInt(ArcTestnetChainID)); e != ErrCustody {
		t.Fatal("registration signer accepted creation")
	}
}
func TestDeploymentJournalPrivateDurableLocked(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "deployment.raw")
	lock, e := deploymentLock(path)
	if e != nil {
		t.Fatal(e)
	}
	if other, e := deploymentLock(path); e == nil {
		other.Close()
		t.Fatal("concurrent deployment admitted")
	}
	original := []byte{1, 2, 3}
	if e = writeDeploymentJournal(path, original); e != nil {
		t.Fatal(e)
	}
	lock.Close()
	lock, e = deploymentLock(path)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	actual, e := privateFile(path, 100)
	if e != nil || string(actual) != string(original) {
		t.Fatal("restart changed journal bytes")
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("journal permissions")
	}
}

type deploymentNetworkAPI struct{ id int64 }

func (a deploymentNetworkAPI) ChainId() *hexutil.Big { return (*hexutil.Big)(big.NewInt(a.id)) }
func TestDeploymentRejectsRPCNetworkMismatch(t *testing.T) {
	for _, id := range []int64{5042, 1, 5042001, 31337, 84532, ArcTestnetChainID} {
		server := rpc.NewServer()
		if e := server.RegisterName("eth", deploymentNetworkAPI{id}); e != nil {
			t.Fatal(e)
		}
		client := ethclient.NewClient(rpc.DialInProc(server))
		e := deploymentNetwork(context.Background(), client)
		client.Close()
		server.Stop()
		if id == ArcTestnetChainID && e != nil {
			t.Fatal(e)
		}
		if id != ArcTestnetChainID && e != ErrDeployment {
			t.Fatalf("RPC network %d accepted", id)
		}
	}
}
