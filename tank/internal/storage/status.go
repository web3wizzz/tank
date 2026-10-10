package storage

import "context"

type Status struct {
	UsedBytes  int64 `json:"used_bytes"`
	ByteLimit  int64 `json:"byte_limit"`
	UsedFiles  int   `json:"used_files"`
	FileLimit  int   `json:"file_limit"`
	AtCapacity bool  `json:"at_capacity"`
}

// Status exposes aggregate capacity only, never paths or shard identifiers.
func (f *Filesystem) Status(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.root.Stat("."); err != nil {
		return Status{}, err
	}
	return Status{UsedBytes: f.usedBytes, ByteLimit: f.quotaBytes, UsedFiles: f.usedFiles, FileLimit: f.quotaFiles, AtCapacity: (f.quotaBytes > 0 && f.usedBytes >= f.quotaBytes) || (f.quotaFiles > 0 && f.usedFiles >= f.quotaFiles)}, nil
}
