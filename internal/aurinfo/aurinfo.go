package aurinfo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/trace"
)

// Sentinel errors.
var (
	ErrAURClientUnreachable = errors.New("aur: client unreachable")
	ErrAURPackageNotFound   = errors.New("aur: package not found")
)

// ScalarRe matches plain PKGBUILD scalar assignments (epoch, pkgver,
// pkgrel). Reuses the pattern from internal/analyzer/pkgbuild.go:358
// against raw file lines — no diff-prefix stripping needed.
var ScalarRe = regexp.MustCompile(`^\s*(epoch|pkgver|pkgrel)\s*=\s*(.+?)\s*$`)

// VersionPair holds the AUR-reported and locally-parsed version
// information for a single package.
type VersionPair struct {
	Pkg domain.PackageName
	// AURVersion is the version string reported by the AUR RPC, or ""
	// if the package was not found in the bulk response.
	AURVersion string
	// LocalVersion is the version string parsed from the local PKGBUILD,
	// or "" if parsing failed or the file does not exist.
	LocalVersion string
	// AURStale reports whether the AUR version differs from the local
	// one — not whether it is newer: the reviewed commit comes from the
	// AUR's own history, so any difference is a later AUR commit
	// (workflow.md § Local Version, #433). True only when both sides
	// have a parsed value.
	AURStale bool
	// AURUnknown is true when the AUR call could not confirm this
	// package (deleted, renamed, or RPC error).  An unknown result
	// is never treated as "up to date."
	AURUnknown bool
	// LocalUnknown is true when the local version could not be resolved;
	// LocalErr says why.
	LocalUnknown bool
	// LocalErr is the resolver's reason the local version is unknown
	// (LocalVersionResolver), nil when it is known (#402).
	LocalErr error
}

// CheckResult bundles the bulk check output and any transient error.
// When Err is non-nil the entire call failed and all packages should
// be treated as unknown.
type CheckResult struct {
	Pairs []VersionPair
	Err   error
}

// Doer abstracts http.Client.Do for testability.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client performs bulk AUR RPC info lookups.
type Client struct {
	baseURL      string
	timeout      time.Duration
	client       Doer
	resolveLocal LocalVersionResolver
}

// NewClient creates a bulk check client. baseURL is typically
// "https://aur.archlinux.org/rpc/". timeout caps each AUR RPC request
// made by FetchAURVersions (a shorter deadline on the caller's context
// still wins; 0 means no cap of its own).
// resolveLocal resolves the local PKGBUILD version for each package
// using git-committed content per workflow.md § Local Version —
// Git-Committed, Not Working Tree.
func NewClient(baseURL string, timeout time.Duration, resolveLocal LocalVersionResolver) *Client {
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		timeout:      timeout,
		client:       http.DefaultClient,
		resolveLocal: resolveLocal,
	}
}

// BulkCheck queries the AUR RPC v5 "info" endpoint for all given
// package names in a single HTTP call, then compares each result
// against the locally parsed PKGBUILD.
func (c *Client) BulkCheck(ctx context.Context, pkgs []domain.PackageName) CheckResult {
	if len(pkgs) == 0 {
		return CheckResult{}
	}

	aurVersions, err := c.FetchAURVersions(ctx, pkgs)
	if err != nil {
		return CheckResult{Err: err}
	}

	var pairs []VersionPair
	for _, pkg := range pkgs {
		pair := VersionPair{Pkg: pkg}

		aurVer, found := aurVersions[pkg]
		if found {
			pair.AURVersion = aurVer
		} else {
			pair.AURUnknown = true
		}

		localVer, localErr := c.resolveLocal(ctx, pkg)
		pair.LocalVersion = localVer
		pair.LocalErr = localErr
		pair.LocalUnknown = localErr != nil

		pair.AURStale, _ = CompareVersions(pair.AURVersion, pair.LocalVersion)

		pairs = append(pairs, pair)
	}

	return CheckResult{Pairs: pairs}
}

// FetchAURVersions performs the HTTP round-trip to the AUR RPC endpoint
// and returns a map of package name to AUR version string. Packages not
// found in the AUR response are absent from the map. Returns an error
// only when the HTTP call itself fails (network, HTTP error code, or
// malformed response). The whole round-trip, including reading the body,
// runs under the client's timeout (see NewClient).
func (c *Client) FetchAURVersions(ctx context.Context, pkgs []domain.PackageName) (map[domain.PackageName]string, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	url := c.buildURL(pkgs)
	trace.Log(ctx, "aurinfo", "cmd", "FetchAURVersions", "url", url, "pkgCount", len(pkgs))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %w", ErrAURClientUnreachable, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAURClientUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %w", ErrAURClientUnreachable, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrAURClientUnreachable, resp.StatusCode)
	}

	var rpcResponse aurRPCResponse
	if err := json.Unmarshal(body, &rpcResponse); err != nil {
		return nil, fmt.Errorf("%w: decode response: %w", ErrAURClientUnreachable, err)
	}

	aurMap := make(map[string]string, len(rpcResponse.Results))
	for _, r := range rpcResponse.Results {
		aurMap[r.Name] = r.Version
	}

	result := make(map[domain.PackageName]string, len(pkgs))
	for _, pkg := range pkgs {
		if ver, found := aurMap[string(pkg)]; found {
			result[pkg] = ver
		}
	}

	return result, nil
}

// CompareVersions compares an AUR-reported version against a locally
// parsed version. It returns (stale, unknown) where:
//
// stale is true when both versions are known and not equal (case-insensitive).
// unknown is true when either side has no version string (empty).
//
// CompareVersions has no I/O — it operates only on already-known version
// strings. It is intended for use by callers that have already performed
// their own AUR fetch and local PKGBUILD parsing.
func CompareVersions(aurVersion, localVersion string) (stale, unknown bool) {
	if aurVersion == "" || localVersion == "" {
		return false, true
	}
	return !strings.EqualFold(localVersion, aurVersion), false
}

// buildURL constructs the bulk AUR RPC info URL with arg[]= parameters.
func (c *Client) buildURL(pkgs []domain.PackageName) string {
	var sb strings.Builder
	sb.WriteString(c.baseURL)
	sb.WriteString("?v=5&type=info")
	for _, pkg := range pkgs {
		sb.WriteString("&arg[]=")
		sb.WriteString(string(pkg))
	}
	return sb.String()
}

// ExtractScalar scans raw text and returns the first scalar value for
// the given name matched by ScalarRe. Returns ("", true) when no match
// is found, when the value is computed rather than assigned (it contains
// a $ or ` expansion, #433). A pkgver() function does not make pkgver
// unknown: the plain assignment is read as for any other package, as the
// AUR does (workflow.md § Local Version, VCS packages, #434).
//
// ExtractScalar is exported so callers (e.g. the update command) can
// parse PKGBUILD content retrieved via git.FileAtCommit without
// duplicating the regex logic.
func ExtractScalar(raw string, name string) (string, bool) {
	val, found, computed := scanScalar(raw, name)
	if !found || computed {
		return "", true
	}
	return val, false
}

// scanScalar finds the first assignment to name. found is false when
// there is none; computed is true when its value is an expansion rather
// than a literal.
func scanScalar(raw string, name string) (val string, found, computed bool) {
	for _, line := range strings.Split(raw, "\n") {
		m := ScalarRe.FindStringSubmatch(line)
		if m == nil || m[1] != name {
			continue
		}
		v := m[2]
		// A $ or ` means the value is computed by the shell (a variable,
		// ${...}, $(...), or the scalar itself), which aur-curator does
		// not evaluate. None of epoch, pkgver or pkgrel can contain these
		// characters literally.
		if strings.ContainsAny(v, "$`") {
			return "", true, true
		}
		return stripPKGBUILDQuotes(v), true, false
	}
	return "", false, false
}

// stripPKGBUILDQuotes removes a single matching pair of surrounding
// single or double quotes from a PKGBUILD scalar value, as a shell
// would when evaluating the assignment. A value with mismatched or
// absent quote characters is returned unchanged.
func stripPKGBUILDQuotes(val string) string {
	if len(val) < 2 {
		return val
	}
	first, last := val[0], val[len(val)-1]
	if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
		return val[1 : len(val)-1]
	}
	return val
}

// extractScalar is a private alias for ExtractScalar, kept for
// internal test compatibility.
func extractScalar(raw string, name string) (string, bool) {
	return ExtractScalar(raw, name)
}

// ExtractFullVersion returns the version as the AUR RPC "Version" field
// writes it: "[epoch:]pkgver-pkgrel", the epoch left out when absent or
// 0 (workflow.md § Local Version, #433). Returns ("", true) when pkgver
// or pkgrel is missing or computed, or when epoch is computed.
func ExtractFullVersion(raw string) (string, bool) {
	pkgver, verUnknown := ExtractScalar(raw, "pkgver")
	if verUnknown {
		return "", true
	}
	pkgrel, relUnknown := ExtractScalar(raw, "pkgrel")
	if relUnknown {
		return "", true
	}
	epoch, found, computed := scanScalar(raw, "epoch")
	if computed {
		return "", true
	}
	v := fmt.Sprintf("%s-%s", pkgver, pkgrel)
	if found && epoch != "" && epoch != "0" {
		v = epoch + ":" + v
	}
	return v, false
}

// aurRPCResponse mirrors the AUR RPC v5 "info" response structure.
type aurRPCResponse struct {
	ResultCount int         `json:"resultcount"`
	Type        string      `json:"type"`
	Version     int         `json:"version"`
	Results     []aurResult `json:"results"`
}

// aurResult is a single package entry in the AUR RPC response.
type aurResult struct {
	Name    string `json:"Name"`
	Version string `json:"Version"`
}
