package siteops

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/realrashid/servlo/internal/config"
	phpDet "github.com/realrashid/servlo/internal/php"
)

// Adding a site from the browser, before anything is registered.
//
// The CLI's link starts from a directory the operator is standing in, so it can
// take the working directory as given and get on with it. A panel handler has
// no working directory worth the name: it gets a path typed into a form, which
// may not exist yet, may be a file, or may be a typo. So the two questions the
// form needs answered come first and separately: what is at this path, and can
// it be made into a site directory.
//
// Neither of these registers anything. Registration is still linker.Resolve and
// linker.Apply, which is where every caller agrees about what a site is.

// DirectoryReport describes a path the add-site form is asking about. Every
// field is what the form should prefill, and an absent directory reports what
// it can rather than failing: the operator is allowed to name a path that does
// not exist yet, which is the whole reason this is separate from linking.
type DirectoryReport struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	HasContent bool   `json:"has_content"`
	// Framework is the detected framework's name, empty when nothing matched or
	// the directory is not there.
	Framework string `json:"framework"`
	// PublicDir is the document root relative to Path, "." when the site is
	// served from its own root.
	PublicDir string `json:"public_dir"`
	// PHPVersion is what the project asks for, empty when it does not say and
	// the caller should fall back to the configured default.
	PHPVersion string `json:"php_version"`
}

// PrepareResult is what PrepareSiteDirectory did.
type PrepareResult struct {
	Path       string
	Created    bool
	HasContent bool
}

// PrepareSiteDirectory makes path usable as a site directory and reports what
// it had to do. An existing directory is left exactly as it is, since pointing
// the panel at a project already on disk is the ordinary case.
//
// One missing level is created. A missing parent is refused: creating a whole
// tree means a mistyped path quietly becomes a new directory somewhere nobody
// will look for it again.
func PrepareSiteDirectory(path string) (PrepareResult, error) {
	if !filepath.IsAbs(path) {
		return PrepareResult{}, fmt.Errorf("%q is not an absolute path: give the whole path, e.g. /home/servlo/sites/example.com", path)
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return PrepareResult{Path: path, HasContent: directoryHasContent(path)}, nil
	case err == nil:
		return PrepareResult{}, fmt.Errorf("%q is a file, not a directory", path)
	case !os.IsNotExist(err):
		return PrepareResult{}, err
	}

	parent := filepath.Dir(path)
	if pinfo, perr := os.Stat(parent); perr != nil || !pinfo.IsDir() {
		return PrepareResult{}, fmt.Errorf("the parent directory %q does not exist: create it first, or point at a path inside a directory that does", parent)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		return PrepareResult{}, fmt.Errorf("creating %q: %w", path, err)
	}
	return PrepareResult{Path: path, Created: true}, nil
}

// InspectSiteDirectory reports what the add-site form should prefill for path.
// It changes nothing, including when the directory is not there.
func InspectSiteDirectory(path string) (DirectoryReport, error) {
	if !filepath.IsAbs(path) {
		return DirectoryReport{}, fmt.Errorf("%q is not an absolute path", path)
	}
	path = filepath.Clean(path)
	report := DirectoryReport{Path: path}

	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return report, nil
	case err != nil:
		return DirectoryReport{}, err
	case !info.IsDir():
		return DirectoryReport{}, fmt.Errorf("%q is a file, not a directory", path)
	}

	report.Exists = true
	report.HasContent = directoryHasContent(path)
	if name, ok := config.DetectFrameworkForDir(path); ok {
		report.Framework = name
	}

	// A framework knows its own document root; only an unrecognised project has
	// to be guessed at by looking for an index.php.
	if proj, _ := config.LoadProjectConfig(path); proj != nil && proj.PublicDir != "" {
		report.PublicDir = proj.PublicDir
	} else if report.Framework != "" {
		if fw, ok := config.GetFrameworkForDir(report.Framework, path); ok {
			report.PublicDir = fw.PublicDir
		}
	}
	if report.PublicDir == "" {
		report.PublicDir = config.DetectPublicDir(path)
	}

	if v, err := phpDet.DetectVersion(path); err == nil {
		report.PHPVersion = v
	}
	return report, nil
}

// directoryHasContent reports whether anything at all is in dir, dotfiles
// included: a bare .git is still a reason to say the directory is not empty.
func directoryHasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
