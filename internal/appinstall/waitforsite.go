package appinstall

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ServloOfficial/servlo/internal/nginx"
)

// siteReadyTimeout bounds the wait. Long enough for nginx to reload and a
// freshly written FPM pool's master to spawn its workers, short enough that an
// install against a genuinely broken site still finishes and says so.
const siteReadyTimeout = 60 * time.Second

// waitForSite blocks until the site's own vhost answers on its own domain.
//
// The application's own installer is driven over HTTP against the site servlo
// has just created, and nothing made sure the site was answering first. Between
// writing the pool and the vhost and reloading them, there is a window where
// nginx has not picked up the new server block and the FPM master has not
// finished spawning the pool's workers. A POST landing in that window gets
// php-fpm's "File not found.", or servlo's own branded catch-all page, and the
// install reports that its setup did not complete.
//
// What that leaves behind is the thing appinstall already refuses to leave
// behind when it cannot drive an installer at all: an uninstalled application,
// serving on a live domain, with its setup form open for the first passer-by to
// complete. The only difference is that here servlo could have driven it and
// was simply too early.
//
// A reply from the site is not the same as the domain replying. Servlo answers
// a domain no site is linked to with a branded page rather than refusing the
// connection, so a wait that took any HTTP response as readiness was satisfied
// by the catch-all and returned before the site's server block existed. The
// catch-all names itself in a header for exactly this, so the wait can tell
// nginx's placeholder from the site.
//
// The other answer that is nginx's rather than the site's is a gateway status:
// the site's server block is live but its FPM pool has no worker on the socket
// yet, which is the second half of the same window.
//
// Past those, any response counts, including a 404 or a 500 the application
// itself produced. The question is whether the site is answering, not what it
// says: an application that has not been set up yet is entitled to answer with
// anything it likes, and judging the body here would mean teaching this
// function what each application looks like before its own installer has run.
func waitForSite(ctx context.Context, siteURL string) error {
	deadline := time.Now().Add(siteReadyTimeout)
	client := &http.Client{Timeout: 5 * time.Second}

	lastReason := "the site never answered"
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, siteURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		switch {
		case err != nil:
			lastReason = err.Error()
		case resp.Header.Get(nginx.CatchAllHeader) == nginx.CatchAllHeaderValue:
			resp.Body.Close()
			lastReason = "the domain was still being served by servlo's catch-all, so nginx had not picked up the site's own vhost"
		case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout:
			resp.Body.Close()
			lastReason = fmt.Sprintf("nginx answered %d, so the site's FPM pool had not come up behind it", resp.StatusCode)
		default:
			resp.Body.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the site did not answer on %s within %s, so its own installer could not be driven: %s",
				siteURL, siteReadyTimeout, lastReason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
