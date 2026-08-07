package appstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Installing an app into a prepared site directory.
//
// The order is the whole design. Each step is undoable only by the step that
// has not happened yet, so the expensive and fallible parts run before anything
// is registered: fetch and verify, then the database, then the config file.
// Registering the site and driving its setup form come last, because those are
// the two that need the site to be servable.
//
// Nothing here knows what any app is. It reads a definition and does what it
// says.

// Deps are the side effects an install needs that live above this package.
// A nil field means the caller cannot do that thing, and the step is refused
// rather than skipped: an app declaring it needs a database and getting none
// would install into a config file pointing at nothing.
type Deps struct {
	// CreateDatabase makes a database and returns the connection the config
	// file should point at.
	CreateDatabase func(name string) (Connection, error)
}

// Connection is where a site's database actually is. It is a value rather than
// an assumption about a local container, because a connection may equally be an
// external managed database (PRD §5.9).
type Connection struct {
	Name     string
	User     string
	Password string
	Host     string
}

// Request is what the operator asked for.
type Request struct {
	// Dir is the site directory, already prepared and empty.
	Dir string
	// SiteURL is what the app should consider its own address.
	SiteURL string
	// DatabaseName is the database to create, when the app wants one.
	DatabaseName string
}

// Result reports what an install produced.
type Result struct {
	// ConfigWritten is the path of the config file, empty when the app has none.
	ConfigWritten string
	// Connection is the database the app was pointed at.
	Connection Connection
}

// Install carries out everything that can be done before the site is served.
//
// Deliberately not the setup form: that needs the site registered and answering
// requests, which is the caller's job and happens after this returns. Splitting
// there keeps this function honest about what it can promise.
func (a App) Install(ctx context.Context, req Request, deps Deps) (Result, error) {
	var res Result

	if err := FetchRelease(ctx, a.Source, req.Dir); err != nil {
		return res, err
	}

	if a.Database.Required {
		if deps.CreateDatabase == nil {
			return res, fmt.Errorf("%s needs a database and none can be created here", a.Label)
		}
		conn, err := deps.CreateDatabase(req.DatabaseName)
		if err != nil {
			return res, fmt.Errorf("creating the database: %w", err)
		}
		res.Connection = conn
	}

	if a.ConfigFile.Template == "" {
		return res, nil
	}

	values := map[string]string{
		"db_name":     res.Connection.Name,
		"db_user":     res.Connection.User,
		"db_password": res.Connection.Password,
		"db_host":     res.Connection.Host,
		"site_url":    req.SiteURL,
	}
	secrets, err := a.GenerateSecrets()
	if err != nil {
		return res, err
	}
	for k, v := range secrets {
		values[k] = v
	}

	rendered, err := a.ConfigFile.Render(values)
	if err != nil {
		return res, err
	}
	path := filepath.Join(req.Dir, filepath.FromSlash(a.ConfigFile.Path))
	// Written at its mode by the open rather than chmodded after, so it is
	// never briefly readable by anything that happened to be watching. It holds
	// the database password.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(a.ConfigFile.Mode()))
	if err != nil {
		return res, fmt.Errorf("writing %s: %w", a.ConfigFile.Path, err)
	}
	if _, err := f.WriteString(rendered); err != nil {
		f.Close()
		return res, fmt.Errorf("writing %s: %w", a.ConfigFile.Path, err)
	}
	if err := f.Close(); err != nil {
		return res, err
	}
	res.ConfigWritten = path
	return res, nil
}
