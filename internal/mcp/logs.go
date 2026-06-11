package mcp

import (
	"encoding/json"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/logsource"
)

func logsTool() mcpTool {
	return mcpTool{
		Name:        "logs",
		Description: "Read logs from any source lerd can reach (app/framework files, PHP-FPM, workers, nginx, dns, services) filtered by string and time. action: sources (list what you can query), fetch (read one source). fetch returns a cursor; call again with since=<cursor> for only the new lines.",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action": {Type: "string", Enum: []string{"sources", "fetch"}},
				"source": {Type: "string", Description: "fetch: a source name from `sources` (e.g. app:laravel.log, fpm, worker:queue, nginx, dns)."},
				"path":   {Type: "string", Description: "Project root. Defaults to the open site."},
				"site":   {Type: "string", Description: "Site name override when not running inside the project."},
				"grep":   {Type: "string", Description: "fetch: keep only lines matching this regex (falls back to literal substring)."},
				"since":  {Type: "string", Description: "fetch: window start — relative (15m, 1h, 2h30m), a timestamp, or a cursor from a prior fetch."},
				"until":  {Type: "string", Description: "fetch: window end (same formats as since)."},
				"level":  {Type: "string", Description: "fetch: app logs only — filter by level (error, warning, info, debug)."},
				"lines":  {Type: "integer", Description: "fetch: max lines to return (default 50)."},
				"cursor": {Type: "string", Description: "fetch: convenience alias for since when polling for new lines."},
			},
			Required: []string{"action"},
		},
	}
}

func execLogsSources(args map[string]any) (any, *rpcError) {
	siteName := strArg(args, "site")
	srcs, err := logsource.Sources(siteName, logsSitePath(args, siteName))
	if err != nil {
		return toolErr(err.Error()), nil
	}
	type srcOut struct {
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Scope  string `json:"scope"`
		Format string `json:"format,omitempty"`
	}
	out := make([]srcOut, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, srcOut{Name: s.Name, Kind: s.Kind.String(), Scope: string(s.Scope), Format: s.Format})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return toolOK(string(b)), nil
}

func execLogsFetch(args map[string]any) (any, *rpcError) {
	source := strArg(args, "source")
	if source == "" {
		return toolErr("source is required — call logs.sources to list available sources"), nil
	}
	siteName := strArg(args, "site")
	src, err := logsource.Resolve(siteName, logsSitePath(args, siteName), source)
	if err != nil {
		return toolErr(err.Error()), nil
	}

	since := strArg(args, "since")
	if c := strArg(args, "cursor"); c != "" {
		since = c
	}
	res, err := logsource.Read(src, logsource.Opts{
		Since: since,
		Until: strArg(args, "until"),
		Grep:  strArg(args, "grep"),
		Lines: intArg(args, "lines", 50),
		Level: strArg(args, "level"),
	})
	if err != nil {
		return toolErr(err.Error()), nil
	}

	out := map[string]any{
		"source":    src.Name,
		"kind":      src.Kind.String(),
		"cursor":    res.Cursor,
		"truncated": res.Truncated,
		"count":     len(res.Entries),
		"entries":   res.Entries,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return toolOK(string(b)), nil
}

// logsSitePath resolves the project path used for source enumeration: an
// explicit path arg, else the registered path of the named site, else the
// injected default site path.
func logsSitePath(args map[string]any, siteName string) string {
	if p := strArg(args, "path"); p != "" {
		return p
	}
	if siteName != "" {
		if s, err := config.FindSite(siteName); err == nil {
			return s.Path
		}
	}
	return defaultSitePath
}
