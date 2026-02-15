package lastfm

import "errors"

func IsRetryable(err error) bool {
	var he HTTPError
	if errors.As(err, &he) {
		// transient upstream failures
		if he.StatusCode >= 500 {
			return true
		}
		// sometimes 429, though Last.fm usually uses API error 29.
		if he.StatusCode == 429 {
			return true
		}
	}

	var ae APIError
	if errors.As(err, &ae) {
		// 29 = Rate limit exceeded
		if ae.Code == 29 {
			return true
		}
	}

	return false
}
