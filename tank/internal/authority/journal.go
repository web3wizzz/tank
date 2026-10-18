// Package authority defines an experimental, unwired authority journal contract.
// It provides integrity checks, not authenticated freshness, semantic mutation
// validation, durable storage, custody fencing or permission to recover/sign.
package authority

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
)

const Version = 1
const MaxPayloadBytes = 1 << 20

var (
	ErrInvalid   = errors.New("invalid authority record")
	ErrConflict  = errors.New("authority compare or operation conflict")
	ErrFenced    = errors.New("authority writer epoch rejected")
	ErrNotFound  = errors.New("authority operation not found")
	ErrUncertain = errors.New("authority append outcome unknown")
)

type Digest [sha256.Size]byte

// Anchor must be independently trusted. StateDigest binds the complete initial
// authoritative state, not only registration records. Hashes do not authenticate it.
type Anchor struct {
	Version       uint32
	Stream        string
	Schema        uint64
	ReleaseDigest Digest
	StateDigest   Digest
}

// Tip is meaningful as a rollback boundary only when obtained freshly from an
// independently authenticated authority. Historical receipts are not current tips.
type Tip struct {
	Sequence uint64
	Epoch    uint64
	Digest   Digest
}

// Request is immutable across retries, including Expected. Payload is the complete
// private mutation; its semantics and resulting StateDigest need a separate validator.
type Request struct {
	Anchor      Anchor
	Expected    Tip
	Epoch       uint64
	OperationID string
	Kind        string
	Payload     []byte
	StateDigest Digest
}

// Record contains private recovery material. Never put records/requests in logs.
type Record struct {
	Request Request
	Tip     Tip
}

// Journal is a proposed linearizable independent authority transport contract.
// No production implementation or caller exists. Implementations must authenticate
// transport and freshly attest Current (including replay protection), validate all
// mutation semantics, and enforce independently granted writer epochs.
//
// Append first resolves an existing OperationID: an exactly identical request
// returns its original immutable receipt even after the tip/epoch advances; any
// changed field conflicts. New IDs require exact Expected and the active epoch.
// Success requires independent durable persistence, never merely a local write.
// Errors other than a definite rejection must be treated as ErrUncertain: resolve
// by retrying the identical request, never allocate a replacement ID/nonce/budget.
// Lookup ErrNotFound does not cancel an in-flight Append or prove it cannot commit.
// Read returns at most limit records after the sequence, without omitting entries;
// recovery must verify them against a separately fetched authenticated current tip.
// Implementations must bound reads and clone payload buffers at ownership boundaries.
// Return sanitized errors without private records, payloads or raw backend details.
// Storage epoch enforcement does not establish custody fencing or permit signing.
type Journal interface {
	Current(context.Context, Anchor) (Tip, error)
	Append(context.Context, Request) (Record, error)
	Lookup(context.Context, Anchor, string) (Record, error)
	Read(ctx context.Context, anchor Anchor, after uint64, limit uint32) ([]Record, error)
}

func validID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// digest uses versioned domain separation and fixed-field JSON structs (no maps,
// floats or caller-supplied JSON). Byte slices use base64; Digest uses byte arrays.
func digest(domain string, value any) Digest {
	body, _ := json.Marshal(value) // Only the fixed supported structs below are used.
	return sha256.Sum256(append([]byte(domain+"\x00"), body...))
}

func Genesis(anchor Anchor) (Tip, error) {
	if anchor.Version != Version || !validID(anchor.Stream) || anchor.Schema == 0 || anchor.ReleaseDigest == (Digest{}) || anchor.StateDigest == (Digest{}) {
		return Tip{}, ErrInvalid
	}
	return Tip{Digest: digest("tank-authority-genesis-v1", anchor)}, nil
}

// Prepare computes a candidate record, NOT a durable acknowledgement. In
// particular it cannot check the independently owned active writer epoch.
func Prepare(req Request) (Record, error) {
	genesis, err := Genesis(req.Anchor)
	if err != nil || !validID(req.OperationID) || !validID(req.Kind) || len(req.Payload) == 0 || len(req.Payload) > MaxPayloadBytes || req.StateDigest == (Digest{}) || req.Expected.Sequence == math.MaxUint64 || req.Epoch == 0 || req.Epoch < req.Expected.Epoch || req.Expected.Digest == (Digest{}) {
		return Record{}, ErrInvalid
	}
	if req.Expected.Sequence == 0 && req.Expected != genesis || req.Expected.Sequence > 0 && req.Expected.Epoch == 0 {
		return Record{}, ErrInvalid
	}
	req.Payload = append([]byte(nil), req.Payload...)
	return Record{Request: req, Tip: Tip{Sequence: req.Expected.Sequence + 1, Epoch: req.Epoch, Digest: digest("tank-authority-record-v1", req)}}, nil
}

// Verify checks a full genesis-to-tip journal without applying any mutations.
// current MUST come from an independently authenticated fresh query, not this log
// or a saved receipt. It checks framing/integrity, not semantic state, freshness,
// signature authenticity or recoverability. All existing restore gates stay shut.
// Memory is proportional to record count; callers must bound acquisition before
// decoding/allocating records. Context cancellation is checked per bounded record.
func Verify(ctx context.Context, anchor Anchor, records []Record, current Tip) error {
	tip, err := Genesis(anchor)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{})
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.Request.Anchor != anchor || record.Request.Expected != tip {
			return ErrConflict
		}
		if _, exists := seen[record.Request.OperationID]; exists {
			return ErrConflict
		}
		prepared, err := Prepare(record.Request)
		if err != nil {
			return err
		}
		if prepared.Tip != record.Tip {
			return ErrInvalid
		}
		seen[record.Request.OperationID] = struct{}{}
		tip = record.Tip
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tip != current {
		return ErrConflict
	}
	return nil
}
