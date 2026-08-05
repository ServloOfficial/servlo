package siteops

import (
	"regexp"
	"strings"
)

// gTLDs lists multi-letter gTLDs stripped from directory names when deriving
// a site handle. All ccTLDs are 2 letters and are handled by ccTLDPattern, so
// we don't need to enumerate them here.
var gTLDs = []string{
	".com", ".net", ".org", ".info", ".biz", ".dev", ".app",
	".tech", ".site", ".online", ".store", ".shop", ".xyz",
	".cloud", ".digital", ".studio", ".agency", ".host", ".ltd",
}

// ccTLDPattern matches any trailing .xx suffix where xx is two ASCII letters.
// ISO 3166 country codes are all 2-letter, so this catches every ccTLD without
// a maintenance list. Only the last segment is stripped; inputs like
// example.co.uk lose only .uk, matching the historical single-pass behaviour.
var ccTLDPattern = regexp.MustCompile(`\.[a-z]{2}$`)

// unsafeNameChars matches characters that must never reach a site handle used
// in systemd unit names and bodies.
var unsafeNameChars = regexp.MustCompile(`[\n\r\x00/]`)

func stripGTLD(name string) (string, bool) {
	for _, ext := range gTLDs {
		if strings.HasSuffix(name, ext) {
			return name[:len(name)-len(ext)], true
		}
	}
	return name, false
}
