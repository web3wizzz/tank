package registry

import (
	"bytes"
	"context"
	"time"
)

// VerifyRuntime gates authorized worker activation on exact compiled registry
// runtime, rather than accepting any nonempty contract at a configured address.
func (r *TestnetReader) VerifyRuntime(ctx context.Context, expected []byte) error {
	if len(expected) == 0 {
		return ErrTestnetRPC
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.verifyNetwork(ctx); err != nil {
		return err
	}
	code, err := r.chain.CodeAt(ctx, r.contract, nil)
	if err != nil || !bytes.Equal(code, expected) {
		return ErrTestnetRPC
	}
	return r.verifyNetwork(ctx)
}
