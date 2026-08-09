package envfile

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// ReadPhpConst reads a WordPress-style wp-config.php file and returns a map
// of the defined PHP constants (define('KEY', 'value') calls).
// Only string and numeric constants are captured; boolean/null defines are ignored.
func ReadPhpConst(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	// Matches: define( 'KEY', 'value' ) or define("KEY", "value") with optional whitespace
	re := regexp.MustCompile(`(?i)define\(\s*['"](\w+)['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
	for _, match := range re.FindAllSubmatch(data, -1) {
		key := string(match[1])
		val := string(match[2])
		result[key] = val
	}
	return result, nil
}

// ApplyPhpConstUpdates rewrites define() values in a PHP file for the given keys.
// Existing define() calls are updated in-place. Keys that don't exist in the file
// are appended before "/* That's all" comment, or at the end of the file if not found.
func ApplyPhpConstUpdates(path string, updates map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := string(data)
	remaining := make(map[string]string)
	for k, v := range updates {
		remaining[k] = v
	}

	// Replace existing defines in-place
	re := regexp.MustCompile(`(?i)(define\(\s*['"])(\w+)(['"]\s*,\s*['"])[^'"]*(['"][^)]*\))`)
	content = re.ReplaceAllStringFunc(content, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) < 5 {
			return match
		}
		key := parts[2]
		if val, ok := remaining[key]; ok {
			delete(remaining, key)
			return parts[1] + key + parts[3] + val + parts[4]
		}
		return match
	})

	// Append remaining (new) keys
	if len(remaining) > 0 {
		var newLines strings.Builder
		for k, v := range remaining {
			newLines.WriteString(fmt.Sprintf("define( '%s', '%s' );\n", k, v))
		}

		// Insert before "/* That's all" if present, otherwise before closing ?>  or at EOF
		insertMarker := "/* That's all"
		if idx := strings.Index(content, insertMarker); idx != -1 {
			content = content[:idx] + newLines.String() + content[idx:]
		} else if idx := strings.LastIndex(content, "?>"); idx != -1 {
			content = content[:idx] + newLines.String() + content[idx:]
		} else {
			content += "\n" + newLines.String()
		}
	}

	if content == string(data) {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// phpConstLiteral matches a define() with any value, quoted or not, so a
// constant whose value is a PHP literal (true, false, a number) can be read and
// rewritten. ReadPhpConst deliberately captures only strings, because an env
// var is a string; a feature flag is not.
var phpConstLiteral = regexp.MustCompile(`(?i)(define\(\s*['"](\w+)['"]\s*,\s*)([^)]*?)(\s*\))`)

// ReadPhpConstLiterals reads every define() in a PHP file with its value
// exactly as written: quotes, `true`, `false`, a number. Callers that need a
// string constant want ReadPhpConst; this is for the ones where the difference
// between false and 'false' is the whole point.
func ReadPhpConstLiterals(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, match := range phpConstLiteral.FindAllStringSubmatch(string(data), -1) {
		out[match[2]] = strings.TrimSpace(match[3])
	}
	return out, nil
}

// ApplyPhpConstLiterals sets define() values verbatim, without adding quotes.
//
// It exists because PHP reads every non-empty string as true, so writing
// 'false' into a flag leaves the flag on while the file appears to say
// otherwise. A caller that means the boolean has to be able to write the
// boolean.
func ApplyPhpConstLiterals(path string, updates map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	remaining := make(map[string]string, len(updates))
	for k, v := range updates {
		remaining[k] = v
	}

	content := phpConstLiteral.ReplaceAllStringFunc(string(data), func(match string) string {
		parts := phpConstLiteral.FindStringSubmatch(match)
		value, ok := remaining[parts[2]]
		if !ok {
			return match
		}
		delete(remaining, parts[2])
		return parts[1] + value + parts[4]
	})

	if len(remaining) > 0 {
		var added strings.Builder
		for _, k := range sortedKeys(remaining) {
			fmt.Fprintf(&added, "define( '%s', %s );\n", k, remaining[k])
		}
		content = insertPhpConst(content, added.String())
	}

	if content == string(data) {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// RemovePhpConsts deletes whole define() lines for the named constants, which
// is how a setting goes back to the framework's own default rather than being
// pinned to the value that happens to match it today.
func RemovePhpConsts(path string, names ...string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[n] = true
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if m := phpConstLiteral.FindStringSubmatch(line); m != nil && wanted[m[2]] && strings.HasPrefix(strings.TrimSpace(line), "define") {
			continue
		}
		kept = append(kept, line)
	}
	content := strings.Join(kept, "\n")
	if content == string(data) {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// insertPhpConst puts new define() lines where WordPress-shaped config files
// expect them: above the "that's all" marker, else above the closing tag, else
// at the end.
func insertPhpConst(content, lines string) string {
	if idx := strings.Index(content, "/* That's all"); idx != -1 {
		return content[:idx] + lines + content[idx:]
	}
	if idx := strings.LastIndex(content, "?>"); idx != -1 {
		return content[:idx] + lines + content[idx:]
	}
	return content + "\n" + lines
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
