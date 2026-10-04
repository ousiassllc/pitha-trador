package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// errNoRelease is latestRelease's result when GitHub reports 404 for the
// latest release.
var errNoRelease = errors.New("no release found")

// latestRelease calls `GET /repos/{owner}/{repo}/releases/latest`; it
// returns errNoRelease when GitHub answers 404.
func (c *Checker) latestRelease(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, c.owner, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Release{}, withKind(ErrorNetwork, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// GitHub answers 404 when the repository has no published release, and
	// also for a repository the caller cannot see; both mean "no release to
	// install" rather than a broken lookup. (An asset download's 404 is still
	// a refusal: its release exists.)
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, errNoRelease
	}
	if err := statusError(resp, "release lookup"); err != nil {
		return Release{}, err
	}

	var release Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseJSONBytes)).Decode(&release); err != nil {
		return Release{}, kindErrorf(ErrorRelease, "decode response: %w", err)
	}
	return release, nil
}

// statusError classifies a non-200 GitHub response to what (the release
// lookup or an asset download), so both fetches report the same ErrorKind
// and Permanent() verdict. It returns nil for 200.
func statusError(resp *http.Response, what string) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	// GitHub signals a rate limit (primary or secondary) as 429, or as
	// 403 with no quota left or a Retry-After: transient, unlike a
	// refused request.
	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden &&
			(resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")) {
		return kindErrorf(ErrorRateLimit, "%s rate limited: status %d", what, resp.StatusCode)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return kindErrorf(ErrorAccess, "%s refused: status %d", what, resp.StatusCode)
	}
	return kindErrorf(ErrorNetwork, "%s: unexpected status %d", what, resp.StatusCode)
}
