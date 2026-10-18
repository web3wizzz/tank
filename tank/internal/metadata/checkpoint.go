package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CheckpointManifest describes bytes, not freshness or authority to activate them.
// Its digest detects damage; it does not authenticate an untrusted destination.
type CheckpointManifest struct {
	Version    int    `json:"version"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	CreatedUTC string `json:"created_utc"`
}

func checkpointPath(path string, existing bool) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil || path == "" {
		return "", errors.New("checkpoint path required")
	}
	parent := filepath.Dir(absolute)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return "", errors.New("checkpoint parent must exist without symlink aliases")
	}
	info, err := os.Lstat(absolute)
	if existing {
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return "", errors.New("checkpoint must be a private regular directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("checkpoint destination already exists or is unavailable")
	}
	return absolute, nil
}

// CreateCheckpoints takes one consistent snapshot and copies the identical bytes
// into new private bundles. Completed bundles survive any later failure. A bundle
// without its final manifest is incomplete and must never be used for recovery.
// Operators must attest independent custody; paths cannot prove separate hosts.
func CreateCheckpoints(ctx context.Context, source string, destinations []string) (completed []string, err error) {
	if len(destinations) < 2 {
		return nil, errors.New("at least two explicit checkpoint destinations required")
	}
	paths := make([]string, len(destinations))
	seen := map[string]bool{}
	for i, path := range destinations {
		paths[i], err = checkpointPath(path, false)
		if err != nil {
			return nil, err
		}
		if seen[paths[i]] {
			return nil, errors.New("checkpoint destinations alias one another")
		}
		parentInfo, statErr := os.Stat(filepath.Dir(paths[i]))
		if statErr != nil {
			return nil, statErr
		}
		for _, earlier := range paths[:i] {
			earlierInfo, statErr := os.Stat(filepath.Dir(earlier))
			if statErr != nil {
				return nil, statErr
			}
			if filepath.Base(earlier) == filepath.Base(paths[i]) && os.SameFile(parentInfo, earlierInfo) {
				return nil, errors.New("checkpoint destinations alias one another")
			}
		}
		seen[paths[i]] = true
	}
	staging, err := os.MkdirTemp(filepath.Dir(paths[0]), ".tank-checkpoint-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging) // Only unpublished operation-owned staging.
	snapshot := filepath.Join(staging, "snapshot.sqlite")
	if err = BackupDatabase(ctx, source, snapshot); err != nil {
		return nil, err
	}
	digest, size, err := checkpointDigest(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	manifest := CheckpointManifest{1, digest, size, time.Now().UTC().Format(time.RFC3339Nano)}
	body, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if err = ctx.Err(); err != nil {
			return completed, err
		}
		if err = os.Mkdir(path, 0700); err != nil {
			return completed, fmt.Errorf("checkpoint publication incomplete: %w", err)
		}
		// Never remove this directory on failure: it may contain the only surviving copy.
		if err = checkpointCopy(ctx, snapshot, filepath.Join(path, "snapshot.sqlite")); err != nil {
			return completed, err
		}
		if err = checkpointWrite(ctx, filepath.Join(path, "manifest.json"), body); err != nil {
			return completed, err
		}
		if err = checkpointSync(path); err != nil {
			return completed, err
		}
		if err = checkpointSync(filepath.Dir(path)); err != nil {
			return completed, err
		}
		if _, err = VerifyCheckpoint(ctx, path, digest); err != nil {
			return completed, err
		}
		completed = append(completed, path)
	}
	return completed, nil
}

func checkpointSync(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func checkpointWrite(ctx context.Context, path string, body []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(body); err != nil {
		return err
	}
	return f.Sync()
}
func checkpointCopy(ctx context.Context, source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, checkpointReader{ctx, in}); err != nil {
		return err
	}
	return out.Sync()
}

type checkpointReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r checkpointReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func checkpointDigest(ctx context.Context, path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", 0, errors.New("checkpoint file must be private and regular")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, checkpointReader{ctx, f})
	return hex.EncodeToString(hash.Sum(nil)), size, err
}

// VerifyCheckpoint verifies completeness and byte integrity only. An optional
// digest pinned in separately trusted custody rejects another valid generation;
// neither an unpinned nor a pinned digest proves no newer mutations exist.
func VerifyCheckpoint(ctx context.Context, path, expectedDigest string) (CheckpointManifest, error) {
	var manifest CheckpointManifest
	absolute, err := checkpointPath(path, true)
	if err != nil {
		return manifest, err
	}
	manifestPath := filepath.Join(absolute, "manifest.json")
	info, err := os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return manifest, errors.New("checkpoint manifest missing or invalid")
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, err
	}
	if err = ctx.Err(); err != nil {
		return manifest, err
	}
	if json.Unmarshal(body, &manifest) != nil || manifest.Version != 1 || manifest.Size <= 0 || len(manifest.SHA256) != 64 {
		return manifest, errors.New("checkpoint manifest invalid")
	}
	if _, err = time.Parse(time.RFC3339Nano, manifest.CreatedUTC); err != nil {
		return manifest, errors.New("checkpoint timestamp invalid")
	}
	digest, size, err := checkpointDigest(ctx, filepath.Join(absolute, "snapshot.sqlite"))
	if err != nil {
		return manifest, err
	}
	if digest != manifest.SHA256 || size != manifest.Size || (expectedDigest != "" && digest != expectedDigest) {
		return manifest, errors.New("checkpoint digest or expected generation mismatch")
	}
	return manifest, nil
}
