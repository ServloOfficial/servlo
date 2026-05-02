package mcp

import (
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
)

// dnsDiagnoseTool returns the MCP tool descriptor. The schema is
// intentionally minimal: callers pass an optional tld override (defaults to
// the registered one) and get back a structured walk of the DNS chain.
func dnsDiagnoseTool() mcpTool {
	return mcpTool{
		Name:        "dns_diagnose",
		Description: "Walk the DNS chain (container, config, port 5300, dig, resolver hookup, interface routing, system lookup) and report which rung is broken. Use when sites don't resolve.",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"tld": {Type: "string", Description: "Defaults to lerd's configured TLD."},
			},
		},
	}
}

func execDNSDiagnose(args map[string]any) (any, *rpcError) {
	tld := strArg(args, "tld")
	if tld == "" {
		if cfg, _ := config.LoadGlobal(); cfg != nil {
			tld = cfg.DNS.TLD
		}
	}
	return dns.Diagnose(tld), nil
}
