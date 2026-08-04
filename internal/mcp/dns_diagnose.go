package mcp

import (
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dns"
)

func execDNSDiagnose(args map[string]any) (any, *rpcError) {
	tld := strArg(args, "tld")
	if tld == "" {
		if cfg, _ := config.LoadGlobal(); cfg != nil {
			tld = cfg.DNS.TLD
		}
	}
	return toolJSON(dns.Diagnose(tld)), nil
}
