package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every `servlo …` a message or a docs page tells somebody to run has to be a
// command that exists.
//
// This is the gate for a failure that is invisible to every other check. A
// message naming a command that was renamed, or one from a feature that was
// deleted, compiles, passes its own tests, and reads as correct to a reviewer.
// It only fails for the operator, and it fails at the worst moment, because
// these strings are mostly error messages: `servlo service preset install <n>`
// appeared in five of them, each firing when something was already broken, and
// cobra answered "accepts at most 1 arg(s), received 2". Alongside it were a
// PHP install command that never existed, a vhost rebuild under a name that was
// never a command, and two references to the deleted DNS stack.
func TestEveryCommandWeTellPeopleToRunExists(t *testing.T) {
	root := &cobra.Command{Use: "servlo"}
	registerCommands(root)
	known := knownCommandPaths(t)
	if len(known) < 100 {
		t.Fatalf("only %d command paths were found, so this check proved nothing", len(known))
	}

	for _, ref := range commandRefsInRepo(t) {
		if !resolves(ref.cmd, root, known) {
			t.Errorf("%s tells the reader to run %q, which is not a servlo command", ref.where, "servlo "+ref.cmd)
		}
	}
}

// knownCommandPaths walks the real command tree, hidden commands included: a
// hidden command is still one a message may legitimately name.
func knownCommandPaths(t *testing.T) map[string]bool {
	t.Helper()
	root := &cobra.Command{Use: "servlo"}
	registerCommands(root)

	out := map[string]bool{}
	var walk func(c *cobra.Command, prefix []string)
	walk = func(c *cobra.Command, prefix []string) {
		for _, sub := range c.Commands() {
			name := strings.Fields(sub.Use)[0]
			path := append(append([]string{}, prefix...), name)
			out[strings.Join(path, " ")] = true
			for _, alias := range sub.Aliases {
				aliasPath := append(append([]string{}, prefix...), alias)
				out[strings.Join(aliasPath, " ")] = true
			}
			walk(sub, path)
		}
	}
	walk(root, nil)
	// Cobra adds these itself once a program runs; registerCommands does not.
	for _, builtin := range []string{"help", "completion", "completion bash", "completion zsh",
		"completion fish", "completion powershell"} {
		out[builtin] = true
	}
	// The framework console passthrough: `servlo artisan …` and friends are
	// dispatched by name to the project's own binary, not registered here.
	for _, passthrough := range []string{"artisan", "console", "sail"} {
		out[passthrough] = true
	}
	return out
}

type commandRef struct {
	cmd   string
	where string
}

// refPatterns find a command inside backticks, and the "run: servlo …" shape
// that error messages and feedback notes use without them.
//
// The colon is what keeps the second one honest. Without it the pattern reads
// ordinary prose ("Run servlo command" as a modal title, "disable servlo
// notifications" in a Short) as an instruction, and a check that cries wolf on
// prose is a check somebody deletes.
//
// So the coverage rule is: a command somebody is told to run goes in backticks,
// or after a colon. Both are already the house style, and the few instructions
// that had neither were given backticks rather than the check being loosened
// around them.
//
// What this deliberately does not cover is a command rendered as bare screen
// text, because the TUI cannot show backticks and a bare string literal
// beginning with "servlo " is indistinguishable from a status line: "servlo
// preset install mysql" and "servlo installation complete" have the same shape.
// A pattern wide enough for the first reports the second, and a check that
// reports prose is a check somebody deletes. The TUI's own hint was one of the
// references this found; it was found by grep, and that is the tool for it.
var refPatterns = []*regexp.Regexp{
	regexp.MustCompile("`servlo ([a-z][^`\n]{0,60})`"),
	regexp.MustCompile(`\b(?:run|Run|with|fix|enable|disable|try):\s+servlo ([a-z][a-z0-9:_ %<>-]{0,40})`),
}

// wordLike matches a token worth carrying into the check: a command name, or a
// placeholder standing in for one argument. A flag ends the reference, since
// what follows it is no longer a path.
var wordLike = regexp.MustCompile(`^(?:[a-z][a-z0-9:_-]*|%s|%v|<[a-z][a-z0-9 _-]*>)$`)

func commandRefsInRepo(t *testing.T) []commandRef {
	t.Helper()
	root := repoRootFor(t)

	var refs []commandRef
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && skipDir(info.Name()) {
				return filepath.SkipDir
			}
			return err
		}
		ext := filepath.Ext(p)
		if ext != ".go" && ext != ".md" {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if exemptFromRefs(rel) {
			return nil
		}
		body, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		src := string(body)
		for i, re := range refPatterns {
			// The un-backticked "run: servlo …" shape is only trusted inside Go
			// string literals. In prose the colon is punctuation, and "safe to
			// run: servlo re-fetches a definition …" is a sentence rather than
			// an instruction.
			if i == 1 && ext != ".go" {
				continue
			}
			for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
				raw := strings.TrimSpace(src[m[2]:m[3]])
				var words []string
				for _, w := range strings.Fields(raw) {
					if !wordLike.MatchString(w) {
						break
					}
					words = append(words, w)
				}
				if len(words) == 0 {
					continue
				}
				line := strings.Count(src[:m[0]], "\n") + 1
				refs = append(refs, commandRef{
					cmd:   strings.Join(words, " "),
					where: rel + ":" + itoa(line),
				})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].where < refs[j].where })
	return refs
}

// resolves reports whether a reference names a real command, and whether what
// follows it could be that command's arguments.
//
// The prefix test alone is not enough, and this is the whole reason the check
// exists: `servlo service preset install mysql` has the known prefix
// `service preset`, so a prefix test calls it fine, and cobra answers
// "accepts at most 1 arg(s), received 2". So the trailing words are handed to
// the command's own Args validator, which is the same judgement the binary
// makes. A placeholder counts as one argument, which is what it stands for.
func resolves(cmd string, root *cobra.Command, known map[string]bool) bool {
	parts := strings.Fields(cmd)
	for n := len(parts); n > 0; n-- {
		path := strings.Join(parts[:n], " ")
		if !known[path] {
			continue
		}
		trailing := parts[n:]
		if len(trailing) == 0 {
			// The reference names the command and stops. Whether its arguments
			// were elided is not this check's business: `servlo restore` in a
			// sentence about restoring is not a claim about arity.
			return true
		}
		target := find(root, parts[:n])
		if target == nil || target.Args == nil {
			return true
		}
		if target.Args(target, trailing) == nil {
			return true
		}
		// The validator refuses. That is only this check's business when the
		// refusal is for too many arguments, which is what a stray subcommand
		// looks like. Docs routinely show a command with its arguments elided
		// or only partly written out, and refusing those would make the check
		// noise: `servlo service expose <service>` names two arguments in a
		// sentence that shows one.
		if len(trailing) > 0 && target.Args(target, trailing[:len(trailing)-1]) == nil {
			return false
		}
		return true
	}
	return false
}

// find walks to the command at path, following aliases as the binary does.
func find(root *cobra.Command, path []string) *cobra.Command {
	cur := root
	for _, want := range path {
		var next *cobra.Command
		for _, sub := range cur.Commands() {
			if strings.Fields(sub.Use)[0] == want {
				next = sub
				break
			}
			for _, alias := range sub.Aliases {
				if alias == want {
					next = sub
					break
				}
			}
			if next != nil {
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

// exemptFromRefs are the files that name commands without instructing anybody
// to run one.
//
// The working documents describe the product, including its name and things it
// may never grow: PRD.md's opening paragraph types `servlo deploy` to show what
// the word looks like under the fingers, and deploying is a panel action with
// no command behind it. The scan's own rule list spells deleted commands on
// purpose. A test that asserts on a message is checked by the message itself.
func exemptFromRefs(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "internal/surfacescan/") {
		return true
	}
	switch rel {
	case "PRD.md", "STORY.md", "CLAUDE.md", "CHANGELOG.md", "README.md", "SECURITY.md", "HANDOVER.md":
		return true
	}
	return false
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "vendor", ".vitepress":
		return true
	}
	return false
}

func repoRootFor(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
