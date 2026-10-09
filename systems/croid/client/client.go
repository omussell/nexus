// Package client provides an HTTP client for the CROID service.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client wraps HTTP communication with a CROID service.
type Client struct {
	baseURL string
	client  *http.Client
}

// New creates a Client for the given base URL (e.g. "http://localhost:9090").
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, client: &http.Client{}}
}

// MintRequest is the JSON body sent to the CROID service.
type MintRequest struct {
	CroType  string `json:"cro_type"`
	CroValue string `json:"cro_value"`
	System   string `json:"system"`
}

// MintResponse is the JSON response from the CROID service.
type MintResponse struct {
	Croid     string `json:"croid"`
	CroType   string `json:"cro_type"`
	CroValue  string `json:"cro_value"`
	System    string `json:"system"`
	CreatedAt string `json:"created_at"`
}

// Mint sends a CROID minting request for the given DOI and returns the
// CROID string. It is idempotent: requesting the same (cro_type, cro_value,
// system) triple twice returns the same CROID.
func (c *Client) Mint(ctx context.Context, doi string) (string, error) {
	body, err := json.Marshal(MintRequest{
		CroType:  "DOI",
		CroValue: doi,
		System:   "nauvis",
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/croid", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	var result MintResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("mint %s: HTTP %d", doi, resp.StatusCode)
	}

	return result.Croid, nil
}
