package tank

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxJSONBytes = 8 << 20

// Client can be shared between goroutines.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u == nil {
		return nil, errors.New("invalid coordinator URL")
	}
	if u.Host == "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("coordinator URL must be an HTTP or HTTPS server URL")
	}
	if strings.ContainsAny(cfg.Token, "\r\n") {
		return nil, errors.New("token contains invalid characters")
	}
	if cfg.Timeout < 0 {
		return nil, errors.New("timeout cannot be negative")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Minute
	}

	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http: &http.Client{
			Timeout: cfg.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) request(
	ctx context.Context,
	method, path string,
	body io.Reader,
	authenticated bool,
) (*http.Response, error) {
	if authenticated && c.token == "" {
		return nil, errors.New("an API token is required")
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
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
		return nil, &APIError{
			StatusCode: res.StatusCode,
			Message:    strings.TrimSpace(string(message)),
		}
	}
	return res, nil
}

func validFileID(id string) bool {
	if len(id) != 64 {
		return false
	}
	value, err := hex.DecodeString(id)
	return err == nil && hex.EncodeToString(value) == id
}

func decodeJSON(reader io.Reader, target any) error {
	body, err := io.ReadAll(io.LimitReader(reader, maxJSONBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxJSONBytes {
		return errors.New("API JSON response exceeds size limit")
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("API response must contain exactly one JSON value")
	}
	return nil
}

// Health checks the coordinator without requiring authentication.
func (c *Client) Health(ctx context.Context) error {
	res, err := c.request(ctx, http.MethodGet, "/health", nil, false)
	if err != nil {
		return err
	}
	return res.Body.Close()
}

// Tank stores bytes and checks that the returned ID matches the submitted data.
// Successful storage does not imply completed on-chain registration.
func (c *Client) Tank(ctx context.Context, data []byte) (Manifest, error) {
	if len(data) < 1 || len(data) > MaxFileBytes {
		return Manifest{}, ErrInvalidSize
	}

	payload := bytes.Clone(data)
	hash := sha256.Sum256(payload)
	expectedID := hex.EncodeToString(hash[:])

	res, err := c.request(
		ctx, http.MethodPost, "/tank", bytes.NewReader(payload), true,
	)
	if err != nil {
		return Manifest{}, err
	}
	defer res.Body.Close()

	var manifest Manifest
	if err := decodeJSON(res.Body, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.FileID != expectedID {
		return Manifest{}, fmt.Errorf("returned file ID: %w", ErrIntegrity)
	}
	if manifest.Version != 1 || manifest.Size != int64(len(payload)) {
		return Manifest{}, errors.New("API returned an invalid manifest version or size")
	}
	return manifest, nil
}

// Retrieve returns bytes only after their size and content hash are verified.
func (c *Client) Retrieve(ctx context.Context, id string) ([]byte, error) {
	if !validFileID(id) {
		return nil, ErrInvalidFileID
	}

	res, err := c.request(ctx, http.MethodGet, "/retrieve/"+id, nil, true)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) < 1 || len(data) > MaxFileBytes {
		return nil, ErrInvalidSize
	}

	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != id {
		return nil, ErrIntegrity
	}
	return data, nil
}

// List returns one page of up to 100 IDs.
// Pass an empty cursor for the first page.
func (c *Client) List(ctx context.Context, after string) ([]string, error) {
	if after != "" && !validFileID(after) {
		return nil, ErrInvalidFileID
	}

	path := "/list"
	if after != "" {
		path += "?after=" + url.QueryEscape(after)
	}
	res, err := c.request(ctx, http.MethodGet, path, nil, true)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var ids []string
	if err := decodeJSON(res.Body, &ids); err != nil {
		return nil, err
	}
	if len(ids) > 100 {
		return nil, errors.New("API returned an oversized list page")
	}
	for _, id := range ids {
		if !validFileID(id) {
			return nil, errors.New("API returned an invalid file ID")
		}
	}
	return ids, nil
}

// RegistrationStatus reads the worker's recorded status.
// It does not independently query or continuously revalidate the chain.
func (c *Client) RegistrationStatus(
	ctx context.Context, id string,
) (Registration, error) {
	if !validFileID(id) {
		return Registration{}, ErrInvalidFileID
	}

	res, err := c.request(
		ctx, http.MethodGet, "/registrations/"+id, nil, true,
	)
	if err != nil {
		return Registration{}, err
	}
	defer res.Body.Close()

	var status Registration
	if err := decodeJSON(res.Body, &status); err != nil {
		return Registration{}, err
	}
	if status.FileID != id || status.Attempts < 0 || status.Target == "" {
		return Registration{}, errors.New("API returned an invalid registration response")
	}
	switch status.Status {
	case "not_queued", "pending", "submitted", "registered", "failed":
	default:
		return Registration{}, errors.New("API returned an unknown registration state")
	}
	if status.TransactionHash != "" &&
		(!strings.HasPrefix(status.TransactionHash, "0x") ||
			!validFileID(strings.TrimPrefix(status.TransactionHash, "0x"))) {
		return Registration{}, errors.New("API returned an invalid transaction hash")
	}
	return status, nil
}
