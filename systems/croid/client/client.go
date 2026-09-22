// Package client provides an HTTP client for minting CROIDs via the CROID
// service POST /croid endpoint. Errors from the CROID service are non-fatal:
// the caller may continue processing without a CROID.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// MintBody is the JSON body sent to POST /croid.
type MintBody struct {
	CroType  string `json:"cro_type"`
	CroValue string `json:"cro_value"`
	System   string `json:"system"`
	Record   string `json:"record,omitempty"`
}

// MintResult is the JSON response from POST /croid.
type MintResult struct {
	Croid     string `json:"croid"`
	CroType   string `json:"cro_type"`
	CroValue  string `json:"cro_value"`
	System    string `json:"system"`
	CreatedAt string `json:"created_at"`
}

// Client talks to a CROID service at the given base URL.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a Client targeting base. base should be an origin such as
// "http://croid:8080" and the client will POST to /croid.
func New(base string) *Client {
	return &Client{
		baseURL: base,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Mint sends a request to mint a CROID for the given identity. If record is
// non-empty it is included in the message so consumers (e.g. Vulcanus) can
// process it immediately without a separate HTTP call. Returns a non-nil error
// if the HTTP request fails or the response is non-2xx.
func (c *Client) Mint(ctx context.Context, croType, croValue, system, record string) (*MintResult, error) {
	body, err := json.Marshal(MintBody{
		CroType:  croType,
		CroValue: croValue,
		System:   system,
		Record:   record,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := c.baseURL + "/croid"

	// Retry with exponential backoff for transient failures (CROID uses SQLite
	// with MaxOpenConns(1), so concurrent requests may be rejected).
	var resp *http.Response
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * time.Second
			time.Sleep(backoff)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err = c.http.Do(req)
		if err == nil && resp.StatusCode < 300 {
			break
		}
		if resp != nil {
			resp.Body.Close()
			resp = nil
		}
	}
	if resp == nil {
		return nil, fmt.Errorf("POST %s: after 5 retries", url)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("POST %s: %d %s", url, resp.StatusCode, errBody.Error)
	}

	var result MintResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
