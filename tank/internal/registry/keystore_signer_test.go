package registry

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
)

func keystoreFixture(t *testing.T) (string, string, *offlineSigner) {
	t.Helper()
	signer := signerFixture(t)
	encoded, err := keystore.EncryptKey(&keystore.Key{Id: uuid.New(), Address: signer.Address(), PrivateKey: signer.key}, "test-only-random-custody-password", keystore.LightScryptN, keystore.LightScryptP)
	if err != nil {
		t.Fatal("test keystore encryption failed")
	}
	dir := t.TempDir()
	keyPath, passwordPath := filepath.Join(dir, "wallet.json"), filepath.Join(dir, "password")
	if os.WriteFile(keyPath, encoded, 0600) != nil || os.WriteFile(passwordPath, []byte("test-only-random-custody-password\n"), 0600) != nil {
		t.Fatal("test custody write failed")
	}
	return keyPath, passwordPath, signer
}

func TestKeystoreSignerSignsOfflineAndRejectsUnsafeCustody(t *testing.T) {
	for _, mode := range []string{"valid", "wrong_password", "wrong_account", "public_key_file", "public_password", "symlink", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			keyPath, passPath, fixture := keystoreFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			expected := fixture.Address()
			switch mode {
			case "wrong_password":
				if err := os.WriteFile(passPath, []byte("synthetic-secret-never-echo"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong_account":
				expected = signerFixture(t).Address()
			case "public_key_file":
				if err := os.Chmod(keyPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "public_password":
				if err := os.Chmod(passPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				link := keyPath + ".link"
				if err := os.Symlink(keyPath, link); err != nil {
					t.Fatal(err)
				}
				keyPath = link
			case "cancelled":
				cancel()
			}
			signer, err := OpenKeystoreSigner(ctx, keyPath, passPath, expected)
			if mode != "valid" {
				if !errors.Is(err, ErrCustody) && !errors.Is(err, context.Canceled) {
					t.Fatal("unsafe custody accepted or error leaked")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer signer.Close()
			store := workerStore(t)
			policy := policyFixture(signer)
			job := prepareJob(t, store, policy, "keystore signing", 60_000_000_000)
			if _, err := PrepareTestnetRegistration(preparationContext(t), store, job, policy, signer, termsFixture()); err != nil {
				t.Fatal(err)
			}
			signer.Close()
			// Closing retires the in-memory key; a restart requires private custody.
			if signer.key.D.Sign() != 0 {
				t.Fatal("closed signer retained key scalar")
			}
		})
	}
}
