//go:build arc_integration

package registry

import (
	"context"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"math/big"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArcLocalGuardedDeploymentResume(t *testing.T) {
	connection, chain, endpoint := arcLocalNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	keyPath, passwordPath, generated := keystoreFixture(t)
	signer, err := OpenKeystoreSigner(ctx, keyPath, passwordPath, generated.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	if err := connection.CallContext(ctx, nil, "anvil_setBalance", signer.Address(), hexutil.EncodeBig(new(big.Int).Exp(big.NewInt(10), big.NewInt(19), nil))); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(u))
	defer proxy.Close()
	creation, runtime, err := LoadDeploymentArtifact("../../contracts/out/TankRegistry.sol/TankRegistry.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := DeploymentConfig{ChainID: ArcTestnetChainID, RPCURL: proxy.URL, Sender: signer.Address(), Creation: creation, Runtime: runtime, MaxGas: 3_000_000, MaxFee: big.NewInt(100_000_000_000), MaxTip: big.NewInt(0), MaxCost: new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), Journal: filepath.Join(dir, "deployment.raw")}
	initial, err := deployArc(ctx, cfg, signer, false, proxy.Client())
	if err != nil || initial.Deployed || initial.Transaction != "" {
		t.Fatalf("preflight: %v", err)
	}
	nonce, err := chain.PendingNonceAt(ctx, signer.Address())
	if err != nil {
		t.Fatal(err)
	}
	if nonce != 0 {
		t.Fatal("preflight broadcast")
	}
	deployed, err := deployArc(ctx, cfg, signer, true, proxy.Client())
	if err != nil || !deployed.Deployed {
		t.Fatalf("deploy: %v", err)
	}
	original, err := privateFile(cfg.Journal, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	signer.Close() // A restart must never call signing for the existing journal.
	if err = connection.CallContext(ctx, nil, "anvil_setBalance", signer.Address(), "0x0"); err != nil {
		t.Fatal(err)
	}
	resumed, err := deployArc(ctx, cfg, signer, true, proxy.Client())
	if err != nil || resumed.Transaction != deployed.Transaction || resumed.Address != deployed.Address || !resumed.Deployed {
		t.Fatalf("resume: %v", err)
	}
	raw, err := privateFile(cfg.Journal, 1<<20)
	if err != nil || string(raw) != string(original) {
		t.Fatal("resume altered transaction bytes")
	}
	nonce, err = chain.PendingNonceAt(ctx, signer.Address())
	if err != nil || nonce != 1 {
		t.Fatal("resume created another transaction")
	}
	cfg.Runtime = append([]byte(nil), runtime...)
	cfg.Runtime[0] ^= 1
	if _, err = deployArc(ctx, cfg, signer, true, proxy.Client()); err != ErrDeployment {
		t.Fatal("incorrect runtime accepted")
	}
}
