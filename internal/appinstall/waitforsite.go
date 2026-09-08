package appinstall

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// siteReadyTimeout bounds the wait. Long enough for nginx to reload and a
// freshly written FPM pool's master to spawn its workers, short enough that an
// install against a genuinely broken site still finishes and says so.
const siteReadyTimeout = 60 * time.Second

// waitForSite blocks until the site answers its own domain over HTTP.
//
// The application's own installer is driven over HTTP against the site servlo
// has just created, and nothing made sure the site was answering first. Between
// writing the pool and the vhost and reloading them, there is a window where
// nginx has not picked up the new server block and the FPM master has not
// finished spawning the pool's workers. A POST landing in that window gets
// php-fpm's "File not found." or nginx's fallback, and the install reports that
// its setup did not complete.
//
// What that leaves behind is the thing appinstall already refuses to leave
// behind when it cannot drive an installer at all: an uninstalled application,
// serving on a live domain, with its setup form open for the first passer-by to
// complete. The only difference is that here servlo could have driven it and
// was simply too early.
//
// Any HTTP response counts as ready, including a 404 or a 500. The question is
// whether the site is answering at all, not what it says: an application that
// has not been set up yet is entitled to answer with anything it likes, and
// judging the body here would mean teaching this function what each application
// looks like before its own installer has run.
func waitForSite(ctx context.Context, siteURL string) error {
	deadline := time.Now().Add(siteReadyTimeout)
	client := &http.Client{Timeout: 5 * time.Second}

	var lastErr error
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, siteURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf("the site did not answer on %s within %s, so its own installer could not be driven: %w",
				siteURL, siteReadyTimeout, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
