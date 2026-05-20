package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"gopkg.in/yaml.v3"
)

// importSeed is a ProjectConfig translated from another local-dev tool's
// project file, used to pre-fill the `lerd init` wizard. label names the
// source file for the confirmation prompt; notes explain what the translation
// dropped or changed so the user can review it before the wizard opens.
type importSeed struct {
	label  string
	config *config.ProjectConfig
	notes  []string
}

// Candidate filenames for each supported tool, in preference order. Shared by
// the per-tool seed functions and detectImportSeed so the prompt label always
// names the file actually found.
var (
	herdFiles  = []string{"herd.yml", "herd.yaml"}
	ddevFiles  = []string{filepath.Join(".ddev", "config.yaml"), filepath.Join(".ddev", "config.yml")}
	landoFiles = []string{".lando.yml", ".lando.yaml"}
)

// detectImportSeed looks for a herd, ddev or lando project file in dir and
// returns a seed translated from the first one found. ok is false when no
// recognised file is present.
func detectImportSeed(dir string) (importSeed, bool) {
	sources := []struct {
		files []string
		parse func(string) (*config.ProjectConfig, []string, bool)
	}{
		{herdFiles, herdSeed},
		{ddevFiles, ddevSeed},
		{landoFiles, landoSeed},
	}
	for _, src := range sources {
		cfg, notes, ok := src.parse(dir)
		if !ok {
			continue
		}
		label := "config file"
		if path, found := findFirst(dir, src.files...); found {
			if rel, err := filepath.Rel(dir, path); err == nil {
				label = rel
			}
		}
		return importSeed{label: label, config: cfg, notes: notes}, true
	}
	return importSeed{}, false
}

// applyImportSeed offers, when interactive and .lerd.yaml does not exist yet,
// to seed the init wizard's defaults from a detected herd/ddev/lando project
// file. It returns existing unchanged when nothing is found or the user
// declines. Every translated value is still shown in the wizard (or printed as
// a note here) so accepting the seed never writes anything unreviewed.
func applyImportSeed(cwd string, existing *config.ProjectConfig) *config.ProjectConfig {
	if !existing.IsEmpty() || !isInteractive() {
		return existing
	}
	seed, ok := detectImportSeed(cwd)
	if !ok {
		return existing
	}
	fmt.Printf("Detected %s. Use it for wizard defaults? [Y/n] ", seed.label)
	var answer string
	fmt.Scanln(&answer) //nolint:errcheck
	if answer != "" && answer[0] != 'Y' && answer[0] != 'y' {
		return existing
	}
	for _, n := range seed.notes {
		fmt.Printf("  - %s\n", n)
	}
	return seed.config
}

// ── shared helpers ───────────────────────────────────────────────────────────

// flexStr decodes any YAML scalar into a string, so an unquoted `php: 8.3`
// parses as cleanly as a quoted `php: '8.3'`.
type flexStr string

func (f *flexStr) UnmarshalYAML(value *yaml.Node) error {
	*f = flexStr(value.Value)
	return nil
}

// findFirst returns the path of the first of names that exists in dir.
func findFirst(dir string, names ...string) (string, bool) {
	for _, n := range names {
		p := filepath.Join(dir, n)
		if fileExists(p) {
			return p, true
		}
	}
	return "", false
}

// collectDomains normalises hostnames into lerd domains: trimmed, lowercased,
// de-duplicated, with empty and wildcard entries removed and order preserved.
func collectDomains(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" || strings.Contains(d, "*") || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// sortedMapKeys returns the keys of m in sorted order, for deterministic output.
func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// splitVersionedName splits a "name:version" string (e.g. "mariadb:11.8") into
// its two parts. A bare "name" yields an empty version.
func splitVersionedName(s string) (name, version string) {
	name, version, _ = strings.Cut(strings.TrimSpace(s), ":")
	return strings.TrimSpace(name), strings.TrimSpace(version)
}

// dbServiceForType maps another tool's database engine name to the lerd service
// the init wizard recognises. MariaDB folds into mysql: lerd's mariadb preset
// is an opt-in alternate the wizard does not offer by default, and the two are
// wire-compatible for local development.
func dbServiceForType(engine string) (service, note string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "mysql":
		return "mysql", "", true
	case "mariadb":
		return "mysql", "database MariaDB mapped to MySQL — run 'lerd service preset mariadb' if you need MariaDB specifically", true
	case "postgres", "postgresql", "pgsql":
		return "postgres", "", true
	}
	return "", "", false
}

// dbVersionNote reports a dropped pinned database version, if any.
func dbVersionNote(version string) string {
	if version == "" {
		return ""
	}
	return fmt.Sprintf("database version %s dropped — lerd resolves service versions per machine", version)
}

// addService appends a ProjectService for name unless one with that name is
// already present, so a config naming the same engine twice yields one entry.
func addService(cfg *config.ProjectConfig, name string) {
	for _, s := range cfg.Services {
		if s.Name == name {
			return
		}
	}
	cfg.Services = append(cfg.Services, config.ProjectService{Name: name})
}

// ── Laravel Herd: herd.yml ───────────────────────────────────────────────────

// herdConfig mirrors the subset of Laravel Herd's herd.yml that maps onto a
// lerd project. The per-service `port` is not parsed: lerd allocates service
// ports per machine, so a pinned Herd port carries no meaning here.
type herdConfig struct {
	Name     string                       `yaml:"name"`
	PHP      flexStr                      `yaml:"php"`
	Secured  bool                         `yaml:"secured"`
	Aliases  []string                     `yaml:"aliases"`
	Services map[string]herdServiceConfig `yaml:"services"`
}

type herdServiceConfig struct {
	Version string `yaml:"version"`
}

// herdServiceToLerd maps Herd service keys to lerd service names. Herd's S3
// service is MinIO; lerd ships RustFS as its S3-compatible store.
var herdServiceToLerd = map[string]string{
	"mysql":       "mysql",
	"postgres":    "postgres",
	"postgresql":  "postgres",
	"redis":       "redis",
	"meilisearch": "meilisearch",
	"minio":       "rustfs",
}

// herdSeed reads herd.yml / herd.yaml from dir and translates it into a
// ProjectConfig. ok is false when no Herd file is present.
func herdSeed(dir string) (*config.ProjectConfig, []string, bool) {
	path, ok := findFirst(dir, herdFiles...)
	if !ok {
		return nil, nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	var hc herdConfig
	if err := yaml.Unmarshal(data, &hc); err != nil {
		return nil, []string{fmt.Sprintf("could not parse %s: %v", filepath.Base(path), err)}, true
	}

	cfg := &config.ProjectConfig{
		PHPVersion: strings.TrimSpace(string(hc.PHP)),
		Secured:    hc.Secured,
	}
	var notes []string

	cfg.Domains = collectDomains(append([]string{hc.Name}, hc.Aliases...))
	if len(cfg.Domains) > 0 {
		notes = append(notes, "domains: "+strings.Join(cfg.Domains, ", "))
	}

	for _, key := range sortedMapKeys(hc.Services) {
		name, mapped := herdServiceToLerd[strings.ToLower(key)]
		if !mapped {
			notes = append(notes, fmt.Sprintf("service %q has no lerd equivalent — skipped", key))
			continue
		}
		addService(cfg, name)
		if v := hc.Services[key].Version; v != "" {
			notes = append(notes, fmt.Sprintf("service %q version %s dropped — lerd resolves service versions per machine", key, v))
		}
	}
	return cfg, notes, true
}

// ── DDEV: .ddev/config.yaml ──────────────────────────────────────────────────

// ddevConfig mirrors the subset of DDEV's .ddev/config.yaml that maps onto a
// lerd project. The framework (`type`) is not translated — lerd auto-detects it.
type ddevConfig struct {
	Name                string       `yaml:"name"`
	PHPVersion          flexStr      `yaml:"php_version"`
	NodeJSVersion       flexStr      `yaml:"nodejs_version"`
	Docroot             string       `yaml:"docroot"`
	Database            ddevDatabase `yaml:"database"`
	AdditionalHostnames []string     `yaml:"additional_hostnames"`
	AdditionalFQDNs     []string     `yaml:"additional_fqdns"`
}

// ddevDatabase accepts both the current scalar form ("mariadb:11.8") and the
// legacy nested form ({type: mariadb, version: "10.11"}).
type ddevDatabase struct {
	Type    string
	Version string
}

func (d *ddevDatabase) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		d.Type, d.Version = splitVersionedName(value.Value)
		return nil
	case yaml.MappingNode:
		var nested struct {
			Type    string `yaml:"type"`
			Version string `yaml:"version"`
		}
		if err := value.Decode(&nested); err != nil {
			return err
		}
		d.Type, d.Version = strings.TrimSpace(nested.Type), strings.TrimSpace(nested.Version)
		return nil
	default:
		return nil
	}
}

// ddevSeed reads .ddev/config.yaml (or .yml) from dir and translates it into a
// ProjectConfig. ok is false when no DDEV config is present.
func ddevSeed(dir string) (*config.ProjectConfig, []string, bool) {
	path, ok := findFirst(dir, ddevFiles...)
	if !ok {
		return nil, nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	var dc ddevConfig
	if err := yaml.Unmarshal(data, &dc); err != nil {
		return nil, []string{fmt.Sprintf("could not parse %s: %v", filepath.Base(path), err)}, true
	}

	cfg := &config.ProjectConfig{
		PHPVersion:  strings.TrimSpace(string(dc.PHPVersion)),
		NodeVersion: strings.TrimSpace(string(dc.NodeJSVersion)),
		PublicDir:   strings.TrimSpace(dc.Docroot),
	}
	var notes []string
	if cfg.PublicDir != "" {
		notes = append(notes, "public_dir: "+cfg.PublicDir)
	}

	cfg.Domains = collectDomains(append([]string{dc.Name}, dc.AdditionalHostnames...))
	if len(cfg.Domains) > 0 {
		notes = append(notes, "domains: "+strings.Join(cfg.Domains, ", "))
	}
	if len(dc.AdditionalFQDNs) > 0 {
		notes = append(notes, "additional_fqdns not imported — add custom domains with 'lerd domain add'")
	}

	if dc.Database.Type != "" {
		if svc, note, mapped := dbServiceForType(dc.Database.Type); mapped {
			addService(cfg, svc)
			if note != "" {
				notes = append(notes, note)
			}
			if n := dbVersionNote(dc.Database.Version); n != "" {
				notes = append(notes, n)
			}
		} else {
			notes = append(notes, fmt.Sprintf("database %q has no lerd equivalent — skipped", dc.Database.Type))
		}
	}
	return cfg, notes, true
}

// ── Lando: .lando.yml ────────────────────────────────────────────────────────

// landoConfig mirrors the subset of a Lando .lando.yml that maps onto a lerd
// project. The recipe is not translated — lerd auto-detects the framework.
type landoConfig struct {
	Name     string                   `yaml:"name"`
	Config   landoRecipeConfig        `yaml:"config"`
	Proxy    map[string][]interface{} `yaml:"proxy"`
	Services map[string]landoService  `yaml:"services"`
}

type landoRecipeConfig struct {
	PHP      flexStr `yaml:"php"`
	Webroot  string  `yaml:"webroot"`
	Database string  `yaml:"database"`
}

type landoService struct {
	Type string `yaml:"type"`
}

// landoServiceTypeToLerd maps Lando service types to lerd service names.
var landoServiceTypeToLerd = map[string]string{
	"redis":         "redis",
	"memcached":     "memcached",
	"elasticsearch": "elasticsearch",
	"mailhog":       "mailpit",
}

// landoSeed reads .lando.yml / .lando.yaml from dir and translates it into a
// ProjectConfig. ok is false when no Lando file is present.
func landoSeed(dir string) (*config.ProjectConfig, []string, bool) {
	path, ok := findFirst(dir, landoFiles...)
	if !ok {
		return nil, nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	var lc landoConfig
	if err := yaml.Unmarshal(data, &lc); err != nil {
		return nil, []string{fmt.Sprintf("could not parse %s: %v", filepath.Base(path), err)}, true
	}

	cfg := &config.ProjectConfig{
		PHPVersion: strings.TrimSpace(string(lc.Config.PHP)),
		PublicDir:  strings.TrimSpace(lc.Config.Webroot),
	}
	var notes []string
	if cfg.PublicDir != "" {
		notes = append(notes, "public_dir: "+cfg.PublicDir)
	}

	// name + proxy hostnames (with the .lndo.site suffix and any :port removed).
	hostnames := []string{lc.Name}
	for _, svc := range sortedMapKeys(lc.Proxy) {
		for _, entry := range lc.Proxy[svc] {
			s, isStr := entry.(string)
			if !isStr {
				continue
			}
			host, _, _ := strings.Cut(s, ":")
			hostnames = append(hostnames, strings.TrimSuffix(host, ".lndo.site"))
		}
	}
	cfg.Domains = collectDomains(hostnames)
	if len(cfg.Domains) > 0 {
		notes = append(notes, "domains: "+strings.Join(cfg.Domains, ", "))
	}

	if lc.Config.Database != "" {
		engine, version := splitVersionedName(lc.Config.Database)
		if svc, note, mapped := dbServiceForType(engine); mapped {
			addService(cfg, svc)
			if note != "" {
				notes = append(notes, note)
			}
			if n := dbVersionNote(version); n != "" {
				notes = append(notes, n)
			}
		} else {
			notes = append(notes, fmt.Sprintf("database %q has no lerd equivalent — skipped", engine))
		}
	}

	// Extra services: a node service contributes a Node version, the rest map
	// to lerd services where an equivalent exists.
	for _, key := range sortedMapKeys(lc.Services) {
		engine, version := splitVersionedName(lc.Services[key].Type)
		if engine == "node" {
			if version != "" && cfg.NodeVersion == "" {
				cfg.NodeVersion = version
			}
			continue
		}
		if name, mapped := landoServiceTypeToLerd[engine]; mapped {
			addService(cfg, name)
		}
	}
	return cfg, notes, true
}
