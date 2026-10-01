package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxFileBytes = 16 << 20

type apiClient struct {
	address string
	token   string
	http    *http.Client
}

func newAPI(address string, timeout time.Duration) (*apiClient, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("--api must be an HTTP or HTTPS server URL")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("--timeout must be positive")
	}

	return &apiClient{
		address: strings.TrimRight(address, "/"),
		token:   os.Getenv("TANK_API_TOKEN"),
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *apiClient) request(
	ctx context.Context,
	method, path string,
	body io.Reader,
	authenticated bool,
) (*http.Response, error) {
	if authenticated && c.token == "" {
		return nil, fmt.Errorf("TANK_API_TOKEN is not set")
	}

	req, err := http.NewRequestWithContext(ctx, method, c.address+path, body)
	if err != nil {
		return nil, err
	}
	if authenticated {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf(
			"API returned HTTP %d: %s",
			res.StatusCode, strings.TrimSpace(string(message)),
		)
	}

	return res, nil
}

func validFileID(id string) bool {
	data, err := hex.DecodeString(id)
	return err == nil && len(data) == sha256.Size &&
		hex.EncodeToString(data) == id
}

func decodeJSON(reader io.Reader, target any) error {
	return json.NewDecoder(io.LimitReader(reader, 8<<20)).Decode(target)
}

// saveVerified publishes the file only after checking its size and hash.
// The hard link creates the destination without overwriting an existing file.
func saveVerified(reader io.Reader, id, destination string) (int64, error) {
	file, err := os.CreateTemp(filepath.Dir(destination), ".tank-download-")
	if err != nil {
		return 0, err
	}
	defer os.Remove(file.Name())
	defer file.Close()

	hash := sha256.New()
	count, err := io.Copy(
		io.MultiWriter(file, hash),
		io.LimitReader(reader, maxFileBytes+1),
	)
	if err != nil {
		return 0, err
	}
	if count < 1 || count > maxFileBytes {
		return 0, fmt.Errorf("download has an invalid size")
	}
	if hex.EncodeToString(hash.Sum(nil)) != id {
		return 0, fmt.Errorf("download failed content-hash verification")
	}

	if err := file.Sync(); err != nil {
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	if err := os.Link(file.Name(), destination); err != nil {
		return 0, fmt.Errorf("cannot create output file: %w", err)
	}

	return count, nil
}
