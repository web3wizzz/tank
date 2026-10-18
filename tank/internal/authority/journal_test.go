package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// syntheticJournal models an independent failure domain for protocol tests ONLY.
// It is memory-only: its success is NOT evidence of disk/off-machine durability.
// No binaries can instantiate this backend, acquire epochs, recover or sign.
type syntheticJournal struct {
	mu                             sync.Mutex
	anchor                         Anchor
	tip                            Tip
	epoch                          uint64
	records                        []Record
	byID                           map[string]Record
	partition, failBefore, loseAck bool
}

var _ Journal = (*syntheticJournal)(nil)

func fixture(t *testing.T) (*syntheticJournal, Request) {
	t.Helper()
	a := Anchor{Version: Version, Stream: "synthetic-pilot", Schema: 8, ReleaseDigest: sha256.Sum256([]byte("synthetic-release")), StateDigest: sha256.Sum256([]byte("synthetic-genesis"))}
	tip, err := Genesis(a)
	if err != nil {
		t.Fatal(err)
	}
	j := &syntheticJournal{anchor: a, tip: tip, epoch: 1, byID: map[string]Record{}}
	return j, Request{Anchor: a, Expected: tip, Epoch: 1, OperationID: "operation-1", Kind: "synthetic-state", Payload: []byte("private-synthetic-payload"), StateDigest: sha256.Sum256([]byte("synthetic-result"))}
}

func clone(r Record) Record { r.Request.Payload = append([]byte(nil), r.Request.Payload...); return r }
func (j *syntheticJournal) Current(ctx context.Context, a Anchor) (Tip, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Tip{}, err
	}
	if j.partition {
		return Tip{}, ErrUncertain
	}
	if a != j.anchor {
		return Tip{}, ErrConflict
	}
	return j.tip, nil
}
func (j *syntheticJournal) Append(ctx context.Context, req Request) (Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if j.partition {
		return Record{}, ErrUncertain
	}
	if req.Anchor != j.anchor {
		return Record{}, ErrConflict
	}
	if old, ok := j.byID[req.OperationID]; ok {
		if !reflect.DeepEqual(old.Request, req) {
			return Record{}, ErrConflict
		}
		return clone(old), nil // Dedup BEFORE comparing tip or active writer epoch.
	}
	if req.Epoch != j.epoch {
		return Record{}, ErrFenced
	}
	if req.Expected != j.tip {
		return Record{}, ErrConflict
	}
	record, err := Prepare(req)
	if err != nil {
		return Record{}, err
	}
	if j.failBefore {
		j.failBefore = false
		return Record{}, ErrUncertain
	}
	j.records = append(j.records, record)
	j.byID[req.OperationID] = record
	j.tip = record.Tip
	if j.loseAck {
		j.loseAck = false
		return Record{}, ErrUncertain
	}
	return clone(record), nil
}
func (j *syntheticJournal) Lookup(ctx context.Context, a Anchor, id string) (Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if j.partition {
		return Record{}, ErrUncertain
	}
	if a != j.anchor {
		return Record{}, ErrConflict
	}
	if record, ok := j.byID[id]; ok {
		return clone(record), nil
	}
	return Record{}, ErrNotFound
}
func (j *syntheticJournal) Read(ctx context.Context, a Anchor, after uint64, limit uint32) ([]Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if j.partition {
		return nil, ErrUncertain
	}
	if a != j.anchor || after > j.tip.Sequence || limit == 0 || limit > 1024 {
		return nil, ErrInvalid
	}
	var out []Record
	for i := after; i < uint64(len(j.records)) && len(out) < int(limit); i++ {
		out = append(out, clone(j.records[i]))
	}
	return out, nil
}

func TestUnknownAcknowledgementAndEpoch(t *testing.T) {
	ctx := context.Background()
	j, req := fixture(t)
	j.failBefore = true
	if _, err := j.Append(ctx, req); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if _, err := j.Lookup(ctx, req.Anchor, req.OperationID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if j.tip != req.Expected {
		t.Fatal("failed persistence advanced tip")
	}
	j.loseAck = true
	if _, err := j.Append(ctx, req); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	original, err := j.Lookup(ctx, req.Anchor, req.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	next := req
	next.Expected = j.tip
	next.OperationID = "operation-2"
	next.Epoch = 2
	j.epoch = 2 // Synthetic storage fencing only; no custody fencing claim.
	if _, err := j.Append(ctx, next); err != nil {
		t.Fatal(err)
	}
	retry, err := j.Append(ctx, req)
	if err != nil || !reflect.DeepEqual(retry, original) || len(j.records) != 2 {
		t.Fatal("retry changed original receipt/history", err)
	}
	changed := req
	changed.Payload = []byte("changed")
	if _, err := j.Append(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	changed = req
	changed.Expected = j.tip
	if _, err := j.Append(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	changed = req
	changed.Epoch = 2
	if _, err := j.Append(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	stale := req
	stale.OperationID = "operation-3"
	stale.Expected = j.tip
	if _, err := j.Append(ctx, stale); !errors.Is(err, ErrFenced) {
		t.Fatal(err)
	}
	j.partition = true
	if _, err := j.Current(ctx, req.Anchor); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if _, err := j.Append(ctx, next); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if _, err := j.Lookup(ctx, req.Anchor, req.OperationID); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if _, err := j.Read(ctx, req.Anchor, 0, 1); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
}

func TestConcurrentCompareAndAppend(t *testing.T) {
	j, req := fixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			r := req
			r.OperationID = id
			_, err := j.Append(context.Background(), r)
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 || len(j.records) != 1 {
		t.Fatal("compare-and-append did not serialize writers")
	}
}

func TestPayloadOwnership(t *testing.T) {
	j, req := fixture(t)
	receipt, err := j.Append(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Payload[0] ^= 1
	receipt.Request.Payload[0] ^= 2
	lookup, _ := j.Lookup(context.Background(), req.Anchor, req.OperationID)
	lookup.Request.Payload[0] ^= 4
	read, _ := j.Read(context.Background(), req.Anchor, 0, 1)
	read[0].Request.Payload[0] ^= 8
	if err := Verify(context.Background(), j.anchor, j.records, j.tip); err != nil {
		t.Fatal("caller changed history", err)
	}
}

func TestVerifyRejectsInvalidHistory(t *testing.T) {
	j, req := fixture(t)
	ctx := context.Background()
	for _, id := range []string{"first", "second", "third"} {
		req.OperationID = id
		req.Expected = j.tip
		if _, err := j.Append(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if err := Verify(ctx, j.anchor, j.records, j.tip); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"payload", "gap", "reorder", "truncation", "extra", "terminal", "anchor", "duplicate-id", "epoch-regression"} {
		t.Run(name, func(t *testing.T) {
			records, _ := j.Read(ctx, j.anchor, 0, 1024)
			anchor, tip := j.anchor, j.tip
			switch name {
			case "payload":
				records[1].Request.Payload[0] ^= 1
			case "gap":
				records = append(records[:1], records[2:]...)
			case "reorder":
				records[0], records[1] = records[1], records[0]
			case "truncation":
				records = records[:2]
			case "extra":
				tip = records[1].Tip
			case "terminal":
				tip.Digest[0] ^= 1
			case "anchor":
				anchor.ReleaseDigest[0] ^= 1
			case "duplicate-id":
				r := records[2].Request
				r.OperationID = records[0].Request.OperationID
				records[2], _ = Prepare(r)
				tip = records[2].Tip
			case "epoch-regression":
				r := records[0].Request
				r.Epoch = 2
				records[0], _ = Prepare(r)
				r = records[1].Request
				r.Expected = records[0].Tip
				records[1].Request = r // Cannot legitimately hash a regressing epoch.
			}
			if err := Verify(ctx, anchor, records, tip); err == nil {
				t.Fatal("invalid history accepted")
			}
		})
	}
	// Integrity alone deliberately cannot distinguish an old log AND old tip.
	// The caller must authenticate a fresh independent tip; never trust a receipt.
	if err := Verify(ctx, j.anchor, j.records[:1], j.records[0].Tip); err != nil {
		t.Fatal(err)
	}
}

func TestValidationBoundsAndCancellation(t *testing.T) {
	j, req := fixture(t)
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Anchor.Version++ },
		func(r *Request) { r.Anchor.Stream = "bad\nstream" },
		func(r *Request) { r.Anchor.Schema = 0 },
		func(r *Request) { r.Anchor.ReleaseDigest = Digest{} },
		func(r *Request) { r.Anchor.StateDigest = Digest{} },
		func(r *Request) { r.OperationID = strings.Repeat("x", 129) },
		func(r *Request) { r.Kind = "" },
		func(r *Request) { r.Payload = nil },
		func(r *Request) { r.Payload = make([]byte, MaxPayloadBytes+1) },
		func(r *Request) { r.StateDigest = Digest{} },
		func(r *Request) { r.Expected.Sequence = ^uint64(0) },
		func(r *Request) { r.Expected.Digest = Digest{} },
		func(r *Request) { r.Expected.Epoch = 1 },
		func(r *Request) { r.Expected.Sequence = 1 },
		func(r *Request) { r.Epoch = 0 },
	} {
		r := req
		mutate(&r)
		if _, err := Prepare(r); err != ErrInvalid {
			t.Fatal("invalid request accepted or sensitive error", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Verify(ctx, j.anchor, nil, j.tip); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := j.Append(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(j.records) != 0 {
		t.Fatal("cancelled append committed")
	}
	if err := Verify(context.Background(), j.anchor, nil, j.tip); err != nil {
		t.Fatal(err)
	}
}

func TestSyntheticPrimaryLossPreservesAcknowledgedState(t *testing.T) {
	// Toy semantics ONLY. Production must validate/replay every metadata mutation
	// and persist signed bytes inside custody before releasing them, not afterwards.
	type state struct {
		Revoked     bool
		Reservation uint64
		Signed      []byte
	}
	j, req := fixture(t)
	ctx := context.Background()
	states := []state{{Revoked: true}, {Revoked: true, Reservation: 8000}, {Revoked: true, Reservation: 8000, Signed: []byte("synthetic-immutable-bytes")}}
	for i, s := range states {
		body, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		req.Expected = j.tip
		req.OperationID = []string{"revoke", "reserve", "retain-bytes"}[i]
		req.Payload = body
		req.StateDigest = sha256.Sum256(body)
		if _, err := j.Append(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	// Only the test's independent backend remains; the primary's state is lost.
	req = Request{}
	tip, err := j.Current(ctx, j.anchor)
	if err != nil {
		t.Fatal(err)
	}
	records, err := j.Read(ctx, j.anchor, 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(ctx, j.anchor, records, tip); err != nil {
		t.Fatal(err)
	}
	var recovered state
	for _, r := range records {
		if sha256.Sum256(r.Request.Payload) != r.Request.StateDigest {
			t.Fatal("state digest mismatch")
		}
		if err := json.Unmarshal(r.Request.Payload, &recovered); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(recovered, states[2]) {
		t.Fatal("acknowledged state lost")
	}
	if err := Verify(ctx, j.anchor, records[:1], tip); err == nil {
		t.Fatal("stale reservation/bytes prefix accepted")
	}
}

func TestDigestFormat(t *testing.T) {
	_, req := fixture(t)
	r, err := Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	// Pin the format across refactoring: changing field names/order is a version change.
	const wantGenesis = "be0b4f607cc624119ccc6778d7caf9fc6d2087fa908280e5ec3657265e9901d0"
	const wantRecord = "68b7102a9239fdc654b0ec5c68523fd73667b69eac2c031ee14e7b9166f331fa"
	if hex.EncodeToString(req.Expected.Digest[:]) != wantGenesis || hex.EncodeToString(r.Tip.Digest[:]) != wantRecord {
		t.Fatalf("format digest mismatch: genesis=%x record=%x", req.Expected.Digest, r.Tip.Digest)
	}
}
