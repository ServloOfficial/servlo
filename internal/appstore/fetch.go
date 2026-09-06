package appstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/siteops"
)

// Fetching a release.
//
// Two properties matter and they are in tension, so the order is deliberate:
// nothing is written into the site until the whole download has been read and
// its checksum has matched. A release that fails verification must not leave a
// single file behind, and the only way to promise that is to hold it in memory,
// check it, and unpack it afterwards.
//
// Holding it in memory is what bounds the size. A release is tens of megabytes;
// the ceiling below is far above any of them and far below anything that would
// trouble a droplet.

// maxReleaseBytes bounds the download. A variable rather than a constant so a
// test can lower it without shipping a hundred megabytes through a test server.
var maxReleaseBytes int64 = 256 << 20

// releaseTimeout bounds the whole fetch, so a stalled mirror fails the install
// rather than holding the request open indefinitely.
const releaseTimeout = 10 * time.Minute

// FetchRelease downloads the release, verifies it against the definition's
// checksum, and unpacks it into dir, which must be empty.
func FetchRelease(ctx context.Context, src Source, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()

	// Checked before the download rather than after it, so a mistake costs a
	// round trip instead of sixty megabytes.
	if err := ensureEmpty(dir); err != nil {
		return err
	}

	body, err := download(ctx, src.URL)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, strings.TrimSpace(src.SHA256)) {
		// Deliberately not "corrupt download": servlo cannot tell a truncated
		// transfer from a substituted release, and the response to both is the
		// same, which is to install neither.
		return fmt.Errorf("the release failed its checksum: the definition pins %s and the download is %s, so servlo will not install it", src.SHA256, got)
	}

	// The archive is a zip because the extractor is already hardened for one,
	// and every project that ships a tarball ships a zip beside it. Reusing
	// that code means an app release gets the same refusals an operator's
	// upload does, rather than a second unpacker with its own oversights.
	if _, err := siteops.Unzip(bytes.NewReader(body), int64(len(body)), dir, siteops.UnzipOptions{
		MaxBytes: maxReleaseBytes,
		MaxFiles: releaseMaxFiles,
		// The definition names the wrapper rather than letting the extractor
		// infer it, so a release that stops shipping one fails loudly instead of
		// quietly installing a directory deeper than the vhost expects.
		StripPrefix: src.StripPrefix,
	}); err != nil {
		return err
	}
	return nil
}

// releaseMaxFiles bounds the entry count. A CMS with its bundled libraries runs
// to tens of thousands of files.
const releaseMaxFiles = 60000

// download reads the whole body, refusing anything over the ceiling.
func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching the release: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching the release: %w", err)
	}
	defer resp.Body.Close()

	// Reported as a status rather than left to fail the checksum: an error page
	// would fail it too, which is the right outcome by a route that reads as a
	// corrupted release rather than a missing one.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching the release: the server answered %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	// Limit+1 so hitting the ceiling exactly is distinguishable from passing it.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the release: %w", err)
	}
	if int64(len(body)) > maxReleaseBytes {
		return nil, fmt.Errorf("the release is larger than the %d bytes servlo will download", maxReleaseBytes)
	}
	return body, nil
}

// ensureEmpty is siteops.Unzip's own precondition, restated here only so the
// caller can be told before a download rather than after one.
func ensureEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("the directory is not empty: an app install creates the site, it does not land on top of one")
	}
	return nil
}
