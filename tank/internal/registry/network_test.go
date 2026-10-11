package registry

import (
	"errors"
	"math/big"
	"testing"
	"time"
)

func TestArcPolicyRejectsFeeFloorAndOtherChainsBeforeSigning(t *testing.T) {
	for _, mode := range []string{"floor", "missing_chain", "base_sepolia", "mainnet"} {
		t.Run(mode, func(t *testing.T) {
			ctx := preparationContext(t)
			store := workerStore(t)
			signer := signerFixture(t)
			policy := policyFixture(signer)
			terms := termsFixture()
			switch mode {
			case "floor":
				terms.FeeWei = big.NewInt(ArcMinimumFee - 1)
			case "missing_chain":
				policy.ChainID = 0
			case "base_sepolia":
				policy.ChainID = 84532
			case "mainnet":
				policy.ChainID = 5042
			}
			job := prepareJob(t, store, policy, mode, time.Minute)
			if _, err := PrepareTestnetRegistration(ctx, store, job, policy, signer, terms); !errors.Is(err, ErrPreparation) || signer.calls.Load() != 0 {
				t.Fatal("unsafe policy reached signer")
			}
		})
	}
}

func TestRegistrationTargetSeparatesChains(t *testing.T) {
	signer := signerFixture(t)
	arc := policyFixture(signer)
	local := arc
	local.ChainID = LocalChainID
	if arc.target() == local.target() {
		t.Fatal("cross-chain registration target collision")
	}
}
