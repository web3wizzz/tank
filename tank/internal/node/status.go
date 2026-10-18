package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"tank.local/tank/internal/storage"
)

func (c *Client) Status(ctx context.Context) (storage.Status, error) {
	var result storage.Status
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/ops/status", nil)
	if err != nil {
		return result, errors.New("node status unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return result, errors.New("node status unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, errors.New("node status unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) > 4096 {
		return result, errors.New("invalid node status")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return result, errors.New("invalid node status")
	}
	// Decode exactly the validated keys. A second struct unmarshal would match
	// case-insensitively, allowing an unknown alias to overwrite these counters.
	for name, destination := range map[string]any{
		"used_bytes": &result.UsedBytes,
		"byte_limit": &result.ByteLimit,
		"used_files": &result.UsedFiles,
		"file_limit": &result.FileLimit,
	} {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, destination) != nil {
			return result, errors.New("invalid node status")
		}
	}
	if result.UsedBytes < 0 || result.ByteLimit < 0 || result.UsedFiles < 0 || result.FileLimit < 0 {
		return result, errors.New("invalid node status")
	}
	// Do not trust an upstream boolean that contradicts its capacity numbers.
	result.AtCapacity = (result.ByteLimit > 0 && result.UsedBytes >= result.ByteLimit) || (result.FileLimit > 0 && result.UsedFiles >= result.FileLimit)
	return result, nil
}
