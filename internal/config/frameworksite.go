package config

// FrameworkForSite resolves the framework definition a site's workers are read
// from, including the one a site with no framework still has.
//
// A custom-container site runs an image the operator brought, so it need not be
// on a framework servlo knows at all, and the long-running processes beside it
// are the custom_workers in its own .servlo.yaml. Two callers built that
// fallback for themselves. Three more needed it and did not have it: the
// panel's start handler refused every worker on such a site while the list it
// was refusing had been assembled by one of the two, the boot sweep never wrote
// their unit files, and start enumerated those units anyway. It is one function
// now, and the tests hold the rest of the tree to using it.
//
// The workers are tagged untrusted, which the hand-rolled fallbacks did not do.
// They come out of a file inside the site's repository, so a host worker among
// them is a command from a git pull asking to run on the server, and that asks
// the operator first everywhere else.
func FrameworkForSite(s *Site) (*Framework, bool) {
	if s == nil {
		return nil, false
	}
	if fw, ok := GetFrameworkForDir(s.Framework, s.Path); ok {
		return fw, true
	}
	if !s.IsCustomContainer() {
		return nil, false
	}
	proj, err := LoadProjectConfig(s.Path)
	if err != nil || proj == nil || len(proj.CustomWorkers) == 0 {
		return nil, false
	}
	workers := make(map[string]FrameworkWorker, len(proj.CustomWorkers))
	for name, w := range proj.CustomWorkers {
		w.ProjectOrigin = true
		workers[name] = w
	}
	return &Framework{Name: "custom", Label: "custom container", Workers: workers}, true
}
