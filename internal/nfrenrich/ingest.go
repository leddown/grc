package nfrenrich

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"syscall"
	"time"
)

// MaxDocumentBytes bounds one source document. Large enough for a full policy
// or standard, small enough that a mistaken upload cannot be read into memory
// and chunked before anyone notices.
const MaxDocumentBytes = 8 << 20 // 8 MiB

// Fetcher retrieves a web document.
//
// This is the module's one outbound request to a caller-supplied address, so it
// is also its one SSRF surface: "analyse this URL" is a request for the server
// to fetch something on the user's behalf, and without a guard that reaches the
// cloud metadata endpoint, an internal admin panel, or anything else the app's
// network position can see but the user cannot.
//
// The guard is applied at connect time via the dialer rather than only to the
// parsed hostname, which is what makes it hold against DNS rebinding: a name
// that resolved to a public address during validation can resolve to 169.254
// microseconds later, and only the dial sees the address actually used.
type Fetcher struct {
	client *http.Client
}

// NewFetcher builds a fetcher that refuses to connect to private address space.
func NewFetcher() *Fetcher {
	// Control runs after DNS resolution with the concrete address the socket is
	// about to connect to. That is the only check that holds against DNS
	// rebinding: a name validated as public can resolve to 169.254.169.254 by
	// the time the dial happens, and the parsed URL never sees it.
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || !isPublicIP(ip) {
				return fmt.Errorf("refusing to fetch from non-public address %s", host)
			}
			return nil
		},
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &Fetcher{client: &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// Each hop is re-validated: an allowed public URL that 302s to
			// http://169.254.169.254/ is the standard bypass.
			return validateFetchURL(req.URL)
		},
	}}
}

// Fetch retrieves url and returns its body and content type.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, "", invalidf("invalid URL")
	}
	if err := validateFetchURL(parsed); err != nil {
		return nil, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "text/html, text/plain, text/markdown;q=0.9, */*;q=0.1")
	req.Header.Set("User-Agent", "GRC-NFR-Enrichment/1.0")

	// #nosec G704 -- the URL is validated for scheme and public address space
	// above, and every redirect hop is re-validated by CheckRedirect.
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching the document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", invalidf("the document URL returned %s", resp.Status)
	}

	// One byte past the limit is read so a document at exactly the cap is not
	// reported as truncated while an oversized one still is.
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxDocumentBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading the document: %w", err)
	}
	if len(body) > MaxDocumentBytes {
		return nil, "", invalidf("the document exceeds the %d MiB limit", MaxDocumentBytes>>20)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

func validateFetchURL(u *neturl.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return invalidf("document URLs must be http or https")
	}
	if u.Host == "" {
		return invalidf("document URL has no host")
	}
	if u.User != nil {
		return invalidf("document URL must not include credentials")
	}
	// A literal private address is rejected here for a clear error message; a
	// hostname that resolves to one is caught at dial time.
	if ip := net.ParseIP(u.Hostname()); ip != nil && !isPublicIP(ip) {
		return invalidf("refusing to fetch from non-public address %s", ip)
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return invalidf("refusing to fetch from localhost")
	}
	return nil
}

// isPublicIP reports whether ip is routable on the public internet. Everything
// else — loopback, private, link-local (which covers the 169.254.169.254 cloud
// metadata endpoint), multicast, unspecified — is refused.
func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Carrier-grade NAT (100.64.0.0/10) is not covered by IsPrivate but is not
	// public either, and is where some managed environments put internal hosts.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	// IPv4-mapped IPv6 (::ffff:127.0.0.1) would otherwise slip past the checks
	// above on the v6 path.
	if v4 := ip.To4(); v4 != nil && !ip.Equal(v4) {
		return isPublicIP(v4)
	}
	return true
}

// PrepareDocument turns raw bytes into a Document and its chunks. It does no
// I/O, so the upload and URL paths converge here and are tested without either.
func PrepareDocument(meta Document, body []byte) (Document, []Chunk, error) {
	if len(body) == 0 {
		return Document{}, nil, ErrEmptyDocument
	}
	if len(body) > MaxDocumentBytes {
		return Document{}, nil, invalidf("the document exceeds the %d MiB limit", MaxDocumentBytes>>20)
	}

	text, err := ExtractText(meta.MediaType, body)
	if err != nil {
		return Document{}, nil, err
	}

	chunks := ChunkText(text)
	if len(chunks) == 0 {
		return Document{}, nil, ErrEmptyDocument
	}

	sum := sha256.Sum256([]byte(text))
	meta.SHA256 = hex.EncodeToString(sum[:])
	meta.ByteSize = int64(len(body))
	meta.ChunkCount = len(chunks)
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = deriveTitle(meta, body, chunks)
	}
	return meta, chunks, nil
}

// deriveTitle picks a human label when the uploader supplied none: the HTML
// <title>, else the filename, else the document's first heading, else the URL.
func deriveTitle(meta Document, body []byte, chunks []Chunk) string {
	if title := HTMLTitle(body); title != "" {
		return title
	}
	if meta.Filename != "" {
		return meta.Filename
	}
	for _, chunk := range chunks {
		if chunk.Heading != "" {
			// The first path segment is the document's own top-level heading.
			return strings.TrimSpace(strings.Split(chunk.Heading, ">")[0])
		}
	}
	if meta.URL != "" {
		return meta.URL
	}
	return "Untitled document"
}
