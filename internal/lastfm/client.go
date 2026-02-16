package lastfm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type Client struct {
	APIKey    string
	Username  string
	UserAgent string
	HTTP      *http.Client
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e HTTPError) Error() string {
	return fmt.Sprintf("lastfm http %d: %s", e.StatusCode, e.Body)
}

type APIError struct {
	Code    int
	Message string
}

func (e APIError) Error() string {
	return fmt.Sprintf("lastfm api error %d: %s", e.Code, e.Message)
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
	q.Set("limit", strconv.Itoa(limit))
	q.Set("page", strconv.Itoa(page))

	var r RecentTracksResponse
	if err := c.doGet(ctx, q, &r); err != nil {
		return Page{}, err
	}
	if r.Error != 0 {
		return Page{}, APIError{Code: r.Error, Message: r.Message}
	}

	p := Page{Tracks: r.RecentTracks.Track}
	p.Page, _ = strconv.Atoi(r.RecentTracks.Attr.Page)
	p.TotalPages, _ = strconv.Atoi(r.RecentTracks.Attr.TotalPages)
	p.Total, _ = strconv.Atoi(r.RecentTracks.Attr.Total)
	return p, nil
}
