package cli

import (
	"fmt"
	"strings"

	"github.com/ServloOfficial/servlo/internal/serverguard"
	"github.com/spf13/cobra"
)

// NewHardenCmd is `servlo harden`: what this server looks like from outside,
// and what to run about it.
func NewHardenCmd() *cobra.Command {
	var sshPort int
	cmd := &cobra.Command{
		Use:   "harden",
		Short: "Audit this server's exposure and print what to fix",
		Long: "Checks what is listening on a public address, whether fail2ban is running, whether " +
			"security updates install themselves, and whether anything servlo stores is readable " +
			"by other accounts on this machine.\n\n" +
			"Nothing here changes anything. Every fix that needs root is printed for you to run, " +
			"because servlo never runs sudo on your behalf.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHarden(sshPort)
		},
	}
	cmd.Flags().IntVar(&sshPort, "ssh-port", 22, "The port sshd listens on, so the firewall plan opens the right one")
	return cmd
}

func runHarden(sshPort int) error {
	findings := serverguard.Audit(serverguard.DefaultWant(sshPort))

	var bad, warn int
	for _, f := range findings {
		switch f.Severity {
		case serverguard.Bad:
			bad++
		case serverguard.Warn:
			warn++
		}
		fmt.Printf("  %s  %s\n", mark(f.Severity), f.Title)
		if f.Detail != "" {
			fmt.Printf("      %s\n", wrapDetail(f.Detail))
		}
		for _, cmd := range f.Fix {
			fmt.Printf("      %s\n", cmd)
		}
		fmt.Println()
	}

	switch {
	case bad > 0:
		fmt.Printf("  %d to fix, %d worth looking at.\n", bad, warn)
		// Not an error exit: the audit ran and told the truth. Failing the
		// command would make it useless in a script that wants the report.
	case warn > 0:
		fmt.Printf("  Nothing urgent, %d worth looking at.\n", warn)
	default:
		fmt.Println("  Nothing to fix.")
	}
	return nil
}

func mark(s serverguard.Severity) string {
	switch s {
	case serverguard.Bad:
		return "✗"
	case serverguard.Warn:
		return "!"
	default:
		return "✓"
	}
}

// wrapDetail keeps a long explanation inside a terminal without depending on
// one being there.
func wrapDetail(text string) string {
	const width = 76
	var out strings.Builder
	col := 0
	for _, word := range strings.Fields(text) {
		if col > 0 && col+len(word)+1 > width {
			out.WriteString("\n      ")
			col = 0
		} else if col > 0 {
			out.WriteString(" ")
			col++
		}
		out.WriteString(word)
		col += len(word)
	}
	return out.String()
}
