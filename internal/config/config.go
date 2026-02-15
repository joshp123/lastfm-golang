package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshp123/lastfm-golang/internal/xdg"
)

type Config struct {
	APIKey       string
	SharedSecret string
	Username     string

	DataDir   string
	Verbose   bool
	UserAgent string
}

type Requirements struct {
	RequireAPIKey   bool
	RequireUsername bool
}

func FromFlags(args []string, req Requirements) (Config, error) {
	fs := flag.NewFlagSet("lastfm-golang", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var c Config
	fs.StringVar(&c.APIKey, "api-key", os.Getenv("LASTFM_API_KEY"), "Last.fm API key (or set LASTFM_API_KEY)")
	fs.StringVar(&c.SharedSecret, "shared-secret", os.Getenv("LASTFM_SHARED_SECRET"), "Last.fm shared secret (or set LASTFM_SHARED_SECRET)")
	fs.StringVar(&c.Username, "user", os.Getenv("LASTFM_USERNAME"), "Last.fm username (or set LASTFM_USERNAME)")
	fs.BoolVar(&c.Verbose, "verbose", false, "Verbose logging")
	fs.StringVar(&c.DataDir, "data-dir", "", "Data directory (default: XDG data dir)")
	fs.StringVar(&c.UserAgent, "user-agent", "lastfm-golang/0 (github.com/joshp123/lastfm-golang)", "HTTP User-Agent")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if req.RequireAPIKey && c.APIKey == "" {
		return Config{}, errors.New("missing api key: set LASTFM_API_KEY or pass --api-key")
	}
	if req.RequireUsername && c.Username == "" {
		return Config{}, errors.New("missing username: set LASTFM_USERNAME or pass --user")
	}

	if c.DataDir == "" {
		h, err := xdg.DataHome()
		if err != nil {
			return Config{}, fmt.Errorf("resolve XDG data home: %w", err)
		}
		c.DataDir = filepath.Join(h, "lastfm-golang")
	}

	return c, nil
}

