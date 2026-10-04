package tank

import (
	"errors"
	"fmt"
	"time"
)

const MaxFileBytes = 16 << 20

var (
	ErrIntegrity     = errors.New("content failed integrity verification")
	ErrInvalidFileID = errors.New("file ID must be 64 lowercase hexadecimal characters")
	ErrInvalidSize   = errors.New("file must contain 1 byte to 16 MiB")
)

type Config struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

type Manifest struct {
	Version   int       `json:"version"`
	FileID    string    `json:"file_id"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Segments  []Segment `json:"segments"`
}

type Segment struct {
	Index      int     `json:"index"`
	Size       int     `json:"size"`
	MerkleRoot string  `json:"merkle_root"`
	Shards     []Shard `json:"shards"`
}

type Shard struct {
	Index   int    `json:"index"`
	Hash    string `json:"hash"`
	NodeURL string `json:"node_url"`
}

type Registration struct {
	FileID          string `json:"file_id"`
	Target          string `json:"target"`
	Status          string `json:"status"`
	TransactionHash string `json:"transaction_hash,omitempty"`
	Attempts        int    `json:"attempts"`
	LastError       string `json:"last_error,omitempty"`
}

// APIError allows callers to inspect an unsuccessful HTTP response.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Tank API returned HTTP %d: %s", e.StatusCode, e.Message)
}
