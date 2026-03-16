package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// Client fetches game reports from the Synod API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	ticker     *time.Ticker
	maxRetries int
}

// NewClient creates a Synod API client with rate limiting.
func NewClient(baseURL string, reqPerSec int) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ticker:     time.NewTicker(time.Second / time.Duration(reqPerSec)),
		maxRetries: 3,
	}
}

// Close stops the rate limiter ticker.
func (c *Client) Close() {
	c.ticker.Stop()
}

// FetchReport fetches a single game report by ID.
// Returns (nil, nil, nil) if the report doesn't exist (404).
func (c *Client) FetchReport(ctx context.Context, id int) (*models.SynodReport, json.RawMessage, error) {
	select {
	case <-c.ticker.C:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	url := fmt.Sprintf("%s/game-report/%d", c.baseURL, id)

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			slog.Debug("retrying fetch", "id", id, "attempt", attempt, "backoff", backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("creating request for %d: %w", id, err)
		}
		req.Header.Set("User-Agent", "TrenchAnalytics/1.0")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetching report %d: %w", id, err)
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB max
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("reading response for %d: %w", id, err)
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			return nil, nil, nil
		}
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error %d for report %d", resp.StatusCode, id)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("unexpected status %d for report %d", resp.StatusCode, id)
		}

		var report models.SynodReport
		if err := json.Unmarshal(body, &report); err != nil {
			return nil, nil, fmt.Errorf("decoding report %d: %w", id, err)
		}

		return &report, json.RawMessage(body), nil
	}

	return nil, nil, lastErr
}
