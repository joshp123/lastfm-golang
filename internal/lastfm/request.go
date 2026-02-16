package lastfm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

func (c Client) doGet(ctx context.Context, q url.Values, out any) error {
	q.Set("api_key", c.APIKey)
	q.Set("format", "json")

	u := url.URL{Scheme: "https", Host: "ws.audioscrobbler.com", Path: "/2.0/", RawQuery: q.Encode()}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HTTPError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decode lastfm response: %w", err)
	}
	return nil
}
