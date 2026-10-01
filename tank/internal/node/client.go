package node

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tank.local/tank/internal/integrity"
	"tank.local/tank/internal/storage"
)

type Client struct {
	BaseURL string
	token   string
	http    *http.Client
}

func NewClient(address, token string) (*Client, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid node URL")
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("node token must contain at least 32 characters")
	}

	return &Client{
		BaseURL: strings.TrimRight(address, "/"),
		token:   token,
		http: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) endpoint(key storage.ShardKey) string {
	return fmt.Sprintf(
		"%s/shards/%s/%d/%d",
		c.BaseURL, key.FileID, key.Segment, key.Index,
	)
}

func (c *Client) Put(
	ctx context.Context,
	key storage.ShardKey,
	data []byte,
	expected integrity.Hash,
) error {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPut, c.endpoint(key), bytes.NewReader(data),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Tank-Shard-Hash", expected.String())
	req.Header.Set("Content-Type", "application/octet-stream")

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("node PUT returned HTTP %d", res.StatusCode)
	}
	return nil
}

// Get verifies bytes against the trusted manifest hash.
func (c *Client) Get(
	ctx context.Context,
	key storage.ShardKey,
	expected integrity.Hash,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, c.endpoint(key), nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node GET returned HTTP %d", res.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(res.Body, storage.MaxShardBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > storage.MaxShardBytes {
		return nil, fmt.Errorf("invalid shard size")
	}
	if integrity.Digest(data) != expected {
		return nil, fmt.Errorf("retrieved shard hash mismatch")
	}

	return data, nil
}
