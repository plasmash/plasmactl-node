package ovh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Client is a thin wrapper over an OAuth2-Bearer http.Client + region base URL.
// We hit ~5 OVH endpoints total — pulling in a heavyweight SDK would be overkill.
type Client struct {
	HTTP    *http.Client
	BaseURL string // e.g. "https://eu.api.ovh.com/1.0"
}

// GetJSON GETs a JSON response from path (relative to BaseURL) into out.
// 404 returns ErrNotFound. Other non-2xx returns a wrapped error with body.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// PostJSON POSTs in (marshaled JSON) and decodes the response into out.
func (c *Client) PostJSON(ctx context.Context, path string, in, out any) error {
	return c.do(ctx, http.MethodPost, path, in, out)
}

// DeleteJSON DELETEs path and decodes response into out.
func (c *Client) DeleteJSON(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodDelete, path, nil, out)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var bodyReader io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("ovh: encode body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ovh: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound{Path: path}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ovh: %s %s: status %d: %s", method, path, resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ErrNotFound signals a 404 from OVH.
type ErrNotFound struct{ Path string }

func (e ErrNotFound) Error() string { return "ovh: not found: " + e.Path }

// IsNotFound reports whether err is or wraps an ErrNotFound.
func IsNotFound(err error) bool {
	var nf ErrNotFound
	return errors.As(err, &nf)
}
