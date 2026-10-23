package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func completeFixture(t *testing.T, populated bool) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic.sqlite")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if !populated {
		return s, path
	}
	ctx := context.Background()
	m := fixture(t)
	if err := s.Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	key, _, err := s.CreateAccessKey(ctx, "synthetic digest user")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.GrantFileAccess(ctx, key.PrincipalID, m.FileID, "encrypted.bin"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFilename(ctx, m.FileID, "legacy.bin"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueRepair(ctx, m.FileID); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueRegistration(ctx, m.FileID, "synthetic-target"); err != nil {
		t.Fatal(err)
	}
	// Structurally valid SQLite rows, NOT real custody/chain evidence. Binary
	// journal bytes are deliberately opaque; this digest cannot validate a signature.
	for _, statement := range []string{
		`INSERT INTO upload_leases VALUES('storage','synthetic-expired-token',0)`,
		`INSERT INTO registration_spend_accounts VALUES(5042002,'synthetic-account','100000000000000000','420000000000000')`,
		`INSERT INTO registration_spend_reservations VALUES(5042002,'synthetic-account','synthetic-hash','420000000000000')`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO registration_transactions VALUES(5042002,'synthetic-account',?,'synthetic-target','18446744073709551615','synthetic-hash',?,'prepared')`, m.FileID, []byte{0, 1, 255, 0}); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func requireComplete(t *testing.T, path string) CompleteStateDigest {
	t.Helper()
	v, err := DigestCompleteState(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCompleteStateSchemaFormat(t *testing.T) {
	s, _ := completeFixture(t, false)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	v, err := completeSchema(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(v[:]) != completeSchemaSHA256 {
		t.Fatalf("synthetic schema vector: %x", v)
	}
}

func TestCompleteStateEmptyFormat(t *testing.T) {
	_, path := completeFixture(t, false)
	v := requireComplete(t, path)
	if v.Version != 1 || v.Schema != 8 || v.Tables != 11 || v.Rows != 0 {
		t.Fatal("wrong complete-state framing")
	}
	const want = "250b23977bf0b80213e087fcdbef568458303a528dfcda4d11877b714ab262bd"
	if hex.EncodeToString(v.SHA256[:]) != want {
		t.Fatalf("synthetic empty-state vector: %x", v.SHA256)
	}
}

func TestCompleteStateCoversEveryAuthoritativeTable(t *testing.T) {
	changes := map[string]string{
		"api_credentials":                 `UPDATE api_credentials SET revoked=1`,
		"file_access":                     `UPDATE file_access SET filename='changed'`,
		"file_names":                      `UPDATE file_names SET filename='changed'`,
		"manifests":                       `UPDATE manifests SET body=body||' '`,
		"principals":                      `UPDATE principals SET label='changed'`,
		"registration_jobs":               `UPDATE registration_jobs SET attempts=attempts+1`,
		"registration_spend_accounts":     `UPDATE registration_spend_accounts SET reserved='420000000000001'`,
		"registration_spend_reservations": `UPDATE registration_spend_reservations SET cost='0420000000000000'`,
		"registration_transactions":       `UPDATE registration_transactions SET raw_tx=X'0001FF01'`,
		"repair_jobs":                     `UPDATE repair_jobs SET lease_until=lease_until+1`,
		"upload_leases":                   `UPDATE upload_leases SET lease_until=lease_until+1`,
	}
	for table, change := range changes {
		t.Run(table, func(t *testing.T) {
			s, path := completeFixture(t, true)
			before := requireComplete(t, path)
			if before.Rows != 11 {
				t.Fatal("fixture must cover all eleven tables")
			}
			if _, err := s.db.Exec(change); err != nil {
				t.Fatal(err)
			}
			after := requireComplete(t, path)
			if before == after {
				t.Fatal("authoritative mutation omitted from digest")
			}
		})
	}
}

func TestCompleteStateIgnoresPhysicalLayoutAndInsertionOrder(t *testing.T) {
	s, path := completeFixture(t, false)
	for _, label := range []string{"binary\x00text", "日本語"} {
		// SQLite permits multiple NULL values in this TEXT PRIMARY KEY. Hash
		// ordering must be deterministic even without a usable unique key.
		if _, err := s.db.Exec(`INSERT INTO principals(id,label) VALUES(NULL,?)`, label); err != nil {
			t.Fatal(err)
		}
	}
	before := requireComplete(t, path)
	if _, err := s.db.Exec(`DELETE FROM principals`); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"日本語", "binary\x00text"} {
		if _, err := s.db.Exec(`INSERT INTO principals(id,label) VALUES(NULL,?)`, label); err != nil {
			t.Fatal(err)
		}
	}
	if before != requireComplete(t, path) {
		t.Fatal("row insertion order changed logical digest")
	}
	// Change physical layout without leaving application state changes behind.
	if _, err := s.db.Exec(`INSERT INTO principals(id,label) VALUES('scratch',?)`, strings.Repeat("x", 32768)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM principals WHERE id='scratch'`); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "vacuum.sqlite")
	if err := s.Backup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	if before != requireComplete(t, backup) {
		t.Fatal("VACUUM changed logical digest")
	}
	if before != requireComplete(t, path) {
		t.Fatal("temporary physical growth changed logical digest")
	}
}

func TestCompleteStateRejectsUnknownSchemaTypesAndBrokenReferences(t *testing.T) {
	for name, mutation := range map[string]string{
		"older":         `PRAGMA user_version=7`,
		"future":        `PRAGMA user_version=9`,
		"application":   `PRAGMA application_id=1`,
		"extra-table":   `CREATE TABLE unreviewed(value TEXT)`,
		"missing-table": `DROP TABLE file_names`,
		"missing-index": `DROP INDEX repair_jobs_due`,
		"extra-trigger": `CREATE TRIGGER unreviewed AFTER INSERT ON principals BEGIN SELECT 1; END`,
		"real-storage":  `UPDATE api_credentials SET expires_at=1.5`,
		"blob-in-text":  `UPDATE principals SET label=X'00FF'`,
		"invalid-utf8":  `UPDATE principals SET label=CAST(X'FF' AS TEXT)`,
		"text-in-blob":  `UPDATE registration_transactions SET raw_tx='opaque-text'`,
		"foreign-key":   `PRAGMA foreign_keys=OFF; UPDATE api_credentials SET principal_id='missing'; PRAGMA foreign_keys=ON;`,
	} {
		t.Run(name, func(t *testing.T) {
			s, path := completeFixture(t, true)
			if _, err := s.db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			v, err := DigestCompleteState(context.Background(), path)
			if err != ErrCompleteState || v != (CompleteStateDigest{}) {
				t.Fatal("unsupported/inconsistent state yielded a digest", err)
			}
		})
	}
}

func TestCompleteStateRejectsUnsupportedTextEncoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-utf16.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA encoding='UTF-16le'; CREATE TABLE seed(value TEXT); DROP TABLE seed`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var encoding string
	if err := s.db.QueryRow("PRAGMA encoding").Scan(&encoding); err != nil || encoding != "UTF-16le" {
		t.Fatal("fixture did not retain UTF-16le", err)
	}
	if v, err := DigestCompleteState(context.Background(), path); err != ErrCompleteState || v != (CompleteStateDigest{}) {
		t.Fatal("unsupported encoding yielded a digest", err)
	}
}

func TestCompleteStateBoundsAndInactiveSourcePreservation(t *testing.T) {
	s, path := completeFixture(t, false)
	if _, err := s.db.Exec(`INSERT INTO principals VALUES('one','value'); INSERT INTO principals VALUES('two','value')`); err != nil {
		t.Fatal(err)
	}
	for _, limits := range []completeLimits{{1, 1 << 20, 64 << 20}, {100000, 1 << 20, 1}, {0, 1, 1}, {100001, 1, 1}} {
		if v, err := digestCompleteState(context.Background(), path, limits); err != ErrCompleteState || v != (CompleteStateDigest{}) {
			t.Fatal("over-limit digest accepted", err)
		}
	}
	if _, err := s.db.Exec(`UPDATE principals SET label=? WHERE id='one'`, strings.Repeat("x", (1<<20)+1)); err != nil {
		t.Fatal(err)
	}
	if v, err := DigestCompleteState(context.Background(), path); err != ErrCompleteState || v != (CompleteStateDigest{}) {
		t.Fatal("oversized text was truncated/accepted", err)
	}
	if _, err := s.db.Exec(`UPDATE principals SET label='value'`); err != nil {
		t.Fatal(err)
	}
	bundles := []string{filepath.Join(t.TempDir(), "replica-a"), filepath.Join(t.TempDir(), "replica-b")}
	if _, err := CreateCheckpoints(context.Background(), path, bundles); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(bundles[0], "snapshot.sqlite")
	before, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := os.ReadFile(candidate + ".restore-gate")
	if err != nil {
		t.Fatal(err)
	}
	if requireComplete(t, path) != requireComplete(t, candidate) {
		t.Fatal("inactive snapshot differs logically")
	}
	after, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	afterGate, err := os.ReadFile(candidate + ".restore-gate")
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) || !bytes.Equal(gate, afterGate) {
		t.Fatal("digest modified inactive source/gate")
	}
	if writable, err := OpenExisting(context.Background(), candidate); err == nil {
		writable.Close()
		t.Fatal("digest activated source")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v, err := DigestCompleteState(ctx, path); !errors.Is(err, context.Canceled) || v != (CompleteStateDigest{}) {
		t.Fatal("cancelled digest yielded state", err)
	}
	missing := filepath.Join(t.TempDir(), "private-do-not-echo", "missing.sqlite")
	if v, err := DigestCompleteState(context.Background(), missing); err != ErrCompleteState || v != (CompleteStateDigest{}) {
		t.Fatal("missing source leaked details or yielded state")
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatal("digest created source parent")
	}
}

func TestCompleteStateSeesCommittedWAL(t *testing.T) {
	s, path := completeFixture(t, true)
	if _, err := s.db.Exec(`PRAGMA wal_autocheckpoint=0; PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	before := requireComplete(t, path)
	mainBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE api_credentials SET revoked=1`); err != nil {
		t.Fatal(err)
	}
	wal, err := os.Stat(path + "-wal")
	if err != nil || wal.Size() <= 32 {
		t.Fatal("fixture did not retain committed WAL", err)
	}
	if before == requireComplete(t, path) {
		t.Fatal("committed WAL revocation omitted")
	}
	mainAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(mainBefore) != sha256.Sum256(mainAfter) {
		t.Fatal("fixture checkpointed WAL or reader modified main database")
	}
}

func TestCompleteStateUsesOneSnapshotDuringConcurrentCommit(t *testing.T) {
	s, path := completeFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	set := func(revoked bool) error {
		label, revocation := "state-a", 0
		if revoked {
			label, revocation = "state-b", 1
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `UPDATE principals SET label=?`, label); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE api_credentials SET revoked=?`, revocation); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := set(false); err != nil {
		t.Fatal(err)
	}
	a := requireComplete(t, path)
	if err := set(true); err != nil {
		t.Fatal(err)
	}
	b := requireComplete(t, path)
	stop, finished := make(chan struct{}), make(chan error, 1)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				finished <- nil
				return
			default:
			}
			if err := set(i%2 == 0); err != nil {
				finished <- err
				return
			}
		}
	}()
	defer func() {
		close(stop)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < 20; i++ {
		v, err := DigestCompleteState(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if v != a && v != b {
			t.Fatal("digest mixed two committed snapshots")
		}
	}
}

func TestCompleteStateFormatMatchesIndependentPython(t *testing.T) {
	for _, populated := range []bool{false, true} {
		s, path := completeFixture(t, populated)
		if populated {
			if _, err := s.db.Exec(`UPDATE upload_leases SET lease_until=-1; UPDATE principals SET label=?`, "日本語\x00exact"); err != nil {
				t.Fatal(err)
			}
		}
		want := requireComplete(t, path)
		// Independent encoding uses Python's SQLite values, signed packing and
		// independently enumerated schema/tables. Only synthetic fixtures enter IPC.
		const script = `
import hashlib,pathlib,sqlite3,struct,sys
u=lambda n:struct.pack('>Q',n)
f=lambda b:u(len(b))+b
text=lambda s:f(s.encode('utf-8'))
db=sqlite3.connect(pathlib.Path(sys.argv[1]).resolve().as_uri()+'?mode=ro',uri=True)
try:
 db.execute('BEGIN')
 objects=db.execute("SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT GLOB 'sqlite_*' ORDER BY type COLLATE BINARY,name COLLATE BINARY").fetchall()
 h=hashlib.sha256(b'tank-complete-schema-v1\0'+u(8)+u(len(objects)))
 for obj in objects:
  for field in obj:h.update(text(field))
 tables=sorted(obj[1] for obj in objects if obj[0]=='table')
 out=hashlib.sha256(b'tank-complete-state-v1\0'+h.digest()+u(len(tables)))
 for table in tables:
  quoted='"'+table.replace('"','""')+'"'
  cols=db.execute('PRAGMA table_info('+quoted+')').fetchall()
  out.update(text(table)+u(len(cols)))
  for col in cols:out.update(text(col[1])+text(col[2]))
  hashes=[]
  for row in db.execute('SELECT * FROM '+quoted):
   rh=hashlib.sha256(b'tank-complete-row-v1\0'+u(len(cols)))
   for v in row:
    if v is None:rh.update(b'\0')
    elif isinstance(v,int):rh.update(b'\1'+struct.pack('>q',v))
    elif isinstance(v,str):rh.update(b'\2'+text(v))
    elif isinstance(v,bytes):rh.update(b'\3'+f(v))
    else:raise ValueError('unsupported fixture storage class')
   hashes.append(rh.digest())
  out.update(u(len(hashes)))
  for rh in sorted(hashes):out.update(rh)
 print(out.hexdigest())
finally:db.close()
`
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "python3", "-c", script, path)
		output, err := cmd.Output()
		cancel()
		if err != nil {
			t.Fatal("independent synthetic encoder failed", err)
		}
		if strings.TrimSpace(string(output)) != hex.EncodeToString(want.SHA256[:]) {
			t.Fatal("Go/Python complete-state formats differ")
		}
	}
}
