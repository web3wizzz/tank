package node

import (
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
	for _, name := range []string{"used_bytes", "byte_limit", "used_files", "file_limit"} {
		if _, ok := fields[name]; !ok {
			return result, errors.New("invalid node status")
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, errors.New("invalid node status")
	}
	if result.UsedBytes < 0 || result.ByteLimit < 0 || result.UsedFiles < 0 || result.FileLimit < 0 {
		return result, errors.New("invalid node status")
	}
	// Do not trust an upstream boolean that contradicts its capacity numbers.
	result.AtCapacity = (result.ByteLimit > 0 && result.UsedBytes >= result.ByteLimit) || (result.FileLimit > 0 && result.UsedFiles >= result.FileLimit)
	return result, nil
}
