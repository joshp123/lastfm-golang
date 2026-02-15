package lastfm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	APIKey    string
	Username  string
	UserAgent string
	HTTP      *http.Client
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

type RecentTracksResponse struct {
	RecentTracks struct {
		Track []Track `json:"track"`
		Attr  struct {
			Page       string `json:"page"`
			PerPage    string `json:"perPage"`
			TotalPages string `json:"totalPages"`
			Total      string `json:"total"`
		} `json:"@attr"`
	} `json:"recenttracks"`

	Error   int    `json:"error"`
	Message string `json:"message"`
}

type TextMBID struct {
	Text string `json:"#text"`
	MBID string `json:"mbid"`
}

type Date struct {
	UTS  string `json:"uts"`
	Text string `json:"#text"`
}

type Track struct {
	Name   string   `json:"name"`
	MBID   string   `json:"mbid"`
	URL    string   `json:"url"`
	Artist TextMBID `json:"artist"`
	Album  TextMBID `json:"album"`
	Date   *Date    `json:"date"`
	Attr   struct {
		NowPlaying string `json:"nowplaying"`
	} `json:"@attr"`
}

type Page struct {
	Tracks     []Track
	Page       int
	TotalPages int
	Total      int
}

func (c Client) GetRecentTracksPage(ctx context.Context, page, limit int) (Page, error) {
	q := url.Values{}
	q.Set("method", "user.getrecenttracks")
	q.Set("user", c.Username)
	q.Set("api_key", c.APIKey)
	q.Set("format", "json")
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", strconv.Itoa(page))

	u := url.URL{Scheme: "https", Host: "ws.audioscrobbler.com", Path: "/2.0/", RawQuery: q.Encode()}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return Page{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Page{}, fmt.Errorf("lastfm http %d: %s", resp.StatusCode, string(b))
	}

	var r RecentTracksResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return Page{}, fmt.Errorf("decode lastfm response: %w", err)
	}
	if r.Error != 0 {
		return Page{}, fmt.Errorf("lastfm api error %d: %s", r.Error, r.Message)
	}

	p := Page{Tracks: r.RecentTracks.Track}
	p.Page, _ = strconv.Atoi(r.RecentTracks.Attr.Page)
	p.TotalPages, _ = strconv.Atoi(r.RecentTracks.Attr.TotalPages)
	p.Total, _ = strconv.Atoi(r.RecentTracks.Attr.Total)
	return p, nil
}
