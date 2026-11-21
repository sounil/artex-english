package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// DefaultRepo is the release source used when neither the Settings field nor ARTEX_UPDATE_REPO sets one.
const DefaultRepo = "sounil/artex-english"

// RepoEnv names the environment variable that sets the release source ("owner/name" on GitHub) when the
// Settings field is empty.
//
// Whoever chooses the source chooses the binary that gets installed. The Settings field is therefore an
// admin-only setting, it only matters once update checks are turned on, and installing still needs an
// explicit click plus a SHA256SUMS match from that release.
const RepoEnv = "ARTEX_UPDATE_REPO"

// Where the effective release source came from (ResolveRepo).
const (
	RepoFromSettings = "settings"
	RepoFromEnv      = "env"
	RepoFromDefault  = "default"
)

// repoPattern accepts GitHub's "owner/name" form: owner up to 39 alphanumerics or hyphens (not leading),
// name up to 100 of letters, digits, '.', '_' or '-'. Anything else could smuggle a path into the API URL.
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)

// ResolveRepo returns the effective release source and where it came from: the Settings value
// (setting) when non-empty, else ARTEX_UPDATE_REPO, else DefaultRepo. An invalid value is an error
// rather than a silent fallback, so a typo never sends update checks somewhere nobody chose.
func ResolveRepo(setting string) (repo, source string, err error) {
	if v := strings.TrimSpace(setting); v != "" {
		repo, err := ValidateRepo(v)
		if err != nil {
			return "", RepoFromSettings, fmt.Errorf("update source in Settings: %w", err)
		}
		return repo, RepoFromSettings, nil
	}
	if v := strings.TrimSpace(os.Getenv(RepoEnv)); v != "" {
		repo, err := ValidateRepo(v)
		if err != nil {
			return "", RepoFromEnv, fmt.Errorf("%s: %w", RepoEnv, err)
		}
		return repo, RepoFromEnv, nil
	}
	return DefaultRepo, RepoFromDefault, nil
}

// ValidateRepo checks a GitHub "owner/name" and returns it trimmed.
func ValidateRepo(v string) (string, error) {
	v = strings.TrimSpace(v)
	if !repoPattern.MatchString(v) || strings.Contains(v, "..") || strings.HasSuffix(v, "/.") {
		return "", fmt.Errorf("%q is not a GitHub repository in owner/name form", v)
	}
	return v, nil
}

// latestURL is GitHub's "latest stable release" endpoint for repo. It skips prereleases and drafts automatically.
func latestURL(repo string) string {
	return "https://api.github.com/repos/" + repo + "/releases/latest"
}

// allowedHosts restricts the domains the upgrade chain may reach. Together with checkRedirect below,
// any hop redirected to a host outside the list fails outright. This is the first gate against DNS poisoning /
// MITM swapping the binary; the second is the SHA256SUMS comparison.
var allowedHosts = map[string]bool{
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true, // object storage where release assets actually live
	"release-assets.githubusercontent.com": true,
	"raw.githubusercontent.com":            true,
}

// Release holds the GitHub Release fields we care about.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Asset is one file attached to a Release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// NewClient builds an HTTP client that only trusts GitHub domains. An empty proxy means direct connection.
//
// Deliberately not reusing the default Transport: the upgrade chain must enforce TLS with certificate
// verification and must not be affected by InsecureSkipVerify set elsewhere.
func NewClient(proxy string) *http.Client {
	tr := &http.Transport{
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if p := strings.TrimSpace(proxy); p != "" {
		if pu, err := url.Parse(p); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Minute, // downloads the whole package; a per-request timeout must not cut it off
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return checkURL(req.URL)
		},
	}
}

// checkURL enforces https plus the domain allowlist.
func checkURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("refusing non-HTTPS URL: %s", u.Scheme+"://"+u.Host)
	}
	if !allowedHosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("refusing non-GitHub domain: %s", u.Hostname())
	}
	return nil
}

// FetchLatest queries the latest stable release of repo ("owner/name", see ResolveRepo).
func FetchLatest(ctx context.Context, c *http.Client, repo string) (*Release, error) {
	repo, err := ValidateRepo(repo)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL(repo), nil)
	if err != nil {
		return nil, err
	}
	if err := checkURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "artex-selfupdate")

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub (a global proxy can be set in system settings): %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests:
		// The unauthenticated GitHub API allows 60 requests per IP per hour; easy to hit behind a shared egress IP.
		return nil, fmt.Errorf("GitHub API rate limit reached (60 per hour). Please try again later")
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("repository %s has no stable release yet", repo)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub responded with status %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to parse Release: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return nil, fmt.Errorf("Release has no tag")
	}
	return &rel, nil
}

// AssetName returns the release package name for the current platform, matching package_binary in build.sh:
// artex-<version>-<os>-<arch>.zip (version without the v prefix).
func AssetName(tag, goos, goarch string) string {
	return fmt.Sprintf("artex-%s-%s-%s.zip", strings.TrimPrefix(tag, "v"), goos, goarch)
}

// FindAsset looks up an asset in the Release by name.
func (r *Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return Asset{}, false
}
