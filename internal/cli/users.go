package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"

	"github.com/realrashid/servlo/internal/auditlog"
	"github.com/realrashid/servlo/internal/authz"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
)

// NewUsersCmd returns the `servlo users` command group: who can sign in to the
// panel.
//
// A shell on the box is the recovery path for everything the panel can lock you
// out of: a forgotten password, an admin account somebody left, a session on a
// laptop that is no longer in the building. These exist so none of those needs
// the panel to be reachable to fix.
//
// Not `servlo auth`, which upstream already uses for sharing SSH keys with the
// containers. Two unrelated meanings of the word under one command would be
// worse than a longer name.
func NewUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage who can sign in to the panel",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return listAccounts() },
	}
	cmd.AddCommand(
		newUsersListCmd(),
		newUsersAddCmd(),
		newUsersPasswordCmd(),
		newUsersRoleCmd(),
		newUsersRemoveCmd(),
		newUsersTOTPCmd(),
	)
	return cmd
}

// NewSessionsCmd returns the `servlo sessions` command group: who is signed in
// right now, and how to end it.
func NewSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List or end signed-in panel sessions",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return listSessions() },
	}
	cmd.AddCommand(newSessionsListCmd(), newSessionsRevokeCmd())
	return cmd
}

func newUsersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the panel accounts",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return listAccounts() },
	}
}

func newUsersAddCmd() *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "add <username>",
		Short: "Add a panel account",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			accounts, err := authz.OpenAccounts()
			if err != nil {
				return err
			}
			password, err := readPasswordTwice()
			if err != nil {
				return err
			}
			created, err := accounts.Create(args[0], password, authz.Role(role))
			if err != nil {
				return err
			}
			auditlog.Record(auditlog.Entry{Action: "users.added", Subject: created.Name, Detail: string(created.Role)})
			feedback.Begin()
			feedback.Done("added " + feedback.Val(created.Name) + " as " + string(created.Role))
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", string(authz.RoleAdmin), "admin or developer")
	return cmd
}

func newUsersPasswordCmd() *cobra.Command {
	var keepSessions bool
	cmd := &cobra.Command{
		Use:   "password <username>",
		Short: "Change an account's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			accounts, err := authz.OpenAccounts()
			if err != nil {
				return err
			}
			password, err := readPasswordTwice()
			if err != nil {
				return err
			}
			if err := accounts.SetPassword(args[0], password); err != nil {
				return err
			}
			auditlog.Record(auditlog.Entry{Action: "users.password.changed", Subject: args[0]})

			feedback.Begin()
			feedback.Done("changed the password for " + feedback.Val(args[0]))

			// Changing a password because it may be known means the sessions
			// opened with it may be too, so they go by default. --keep-sessions
			// is for the ordinary rotation where nothing is suspected.
			if keepSessions {
				feedback.Note("existing sessions were left signed in")
				return nil
			}
			sessions, err := authz.OpenSessions()
			if err != nil {
				return err
			}
			if err := sessions.RevokeUser(args[0]); err != nil {
				return err
			}
			feedback.Note("signed out every session for that account")
			return nil
		},
	}
	cmd.Flags().BoolVar(&keepSessions, "keep-sessions", false, "Leave existing sessions signed in")
	return cmd
}

func newUsersRoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "role <username> <admin|developer>",
		Short: "Change an account's role",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			accounts, err := authz.OpenAccounts()
			if err != nil {
				return err
			}
			if err := accounts.SetRole(args[0], authz.Role(args[1])); err != nil {
				return err
			}
			auditlog.Record(auditlog.Entry{Action: "users.role.changed", Subject: args[0], Detail: args[1]})
			feedback.Begin()
			feedback.Done(args[0] + " is now " + feedback.Val(args[1]))
			return nil
		},
	}
}

func newUsersRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <username>",
		Short: "Remove a panel account",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			accounts, err := authz.OpenAccounts()
			if err != nil {
				return err
			}
			if err := accounts.Delete(args[0]); err != nil {
				return err
			}
			// The account is gone; its sessions have to go with it or the
			// removal is cosmetic until they expire.
			sessions, err := authz.OpenSessions()
			if err != nil {
				return err
			}
			if err := sessions.RevokeUser(args[0]); err != nil {
				return err
			}
			auditlog.Record(auditlog.Entry{Action: "users.removed", Subject: args[0]})
			feedback.Begin()
			feedback.Done("removed " + feedback.Val(args[0]) + " and signed out its sessions")
			return nil
		},
	}
}

func newSessionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List signed-in sessions",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return listSessions() },
	}
}

func newSessionsRevokeCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "End a session, or every session with --all",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			sessions, err := authz.OpenSessions()
			if err != nil {
				return err
			}
			if all {
				if err := sessions.RevokeAll(); err != nil {
					return err
				}
				auditlog.Record(auditlog.Entry{Action: "sessions.revoked", Subject: "all"})
				feedback.Begin()
				feedback.Done("signed out every session")
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("which session? Pass an id from `servlo sessions list`, or --all")
			}
			if err := sessions.Revoke(args[0]); err != nil {
				return err
			}
			auditlog.Record(auditlog.Entry{Action: "sessions.revoked", Subject: args[0]})
			feedback.Begin()
			feedback.Done("ended session " + feedback.Val(args[0]))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "End every session")
	return cmd
}

func listAccounts() error {
	accounts, err := authz.OpenAccounts()
	if err != nil {
		return err
	}
	list := accounts.List()
	feedback.Begin()
	if len(list) == 0 {
		feedback.Line("no accounts yet")
		feedback.Note("the panel offers to create the first one when you open it, or: servlo users add <username>")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tROLE\tCREATED")
	for _, account := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\n", account.Name, account.Role, account.Created.Format("2006-01-02"))
	}
	return w.Flush()
}

func listSessions() error {
	store, err := authz.OpenSessions()
	if err != nil {
		return err
	}
	list := store.List()
	feedback.Begin()
	if len(list) == 0 {
		feedback.Line("nobody is signed in")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUSER\tFROM\tLAST SEEN\tCLIENT")
	for _, session := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			session.ID, session.User, session.IP,
			humanSince(session.LastSeen), shortUserAgent(session.UserAgent))
	}
	return w.Flush()
}

func humanSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// shortUserAgent picks the browser out of a user agent string, which is the
// only part an operator deciding whether a session is theirs will read.
func shortUserAgent(ua string) string {
	for _, name := range []string{"Firefox", "Edg", "Chrome", "Safari"} {
		if strings.Contains(ua, name) {
			if name == "Edg" {
				return "Edge"
			}
			return name
		}
	}
	if ua == "" {
		return "unknown"
	}
	if len(ua) > 24 {
		return ua[:24]
	}
	return ua
}

// AdoptInheritedCredentials turns the username and password hash an install
// carried in its config into a real account, once.
//
// Without this, upgrading into session authentication would present the
// first-run setup form on a machine that already had a password, and the
// operator's existing credentials would silently stop meaning anything.
func AdoptInheritedCredentials() error {
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil {
		return err
	}
	if cfg.UI.PasswordHash == "" {
		return nil
	}
	accounts, err := authz.OpenAccounts()
	if err != nil {
		return err
	}
	if accounts.Any() {
		return nil
	}
	name := cfg.UI.Username
	if name == "" {
		name = "admin"
	}
	if err := accounts.Adopt(name, cfg.UI.PasswordHash, authz.RoleAdmin); err != nil {
		return err
	}
	// The config copy goes, so there is one place a password lives. The hash is
	// in the account store now and the next sign-in upgrades it to Argon2id.
	cfg.UI.PasswordHash = ""
	cfg.UI.Username = ""
	return config.SaveGlobal(cfg)
}

func newUsersTOTPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "totp",
		Short: "Turn the second factor on or off for an account",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newUsersTOTPEnableCmd(), newUsersTOTPDisableCmd(), newUsersTOTPCodesCmd())
	return cmd
}

func newUsersTOTPEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <username>",
		Short: "Enrol an authenticator app for an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			accounts, err := authz.OpenAccounts()
			if err != nil {
				return err
			}
			if _, found := accounts.Lookup(args[0]); !found {
				return fmt.Errorf("no account named %q", args[0])
			}
			secret, err := authz.NewTOTPSecret()
			if err != nil {
				return err
			}
			cfg, _ := config.LoadGlobal()
			issuer := ""
			if cfg != nil {
				issuer = strings.TrimSpace(cfg.UI.Domain)
			}
			uri := authz.TOTPEnrolmentURI(args[0], issuer, secret)

			feedback.Begin()
			feedback.Line("Scan this with your authenticator app:")
			fmt.Println()
			// Rendered in the terminal rather than saved somewhere, because a
			// QR code containing the secret is a file nobody remembers to
			// delete.
			qr, err := qrcode.New(uri, qrcode.Medium)
			if err != nil {
				return err
			}
			fmt.Println(qr.ToSmallString(false))
			feedback.Note("or type the secret in by hand: " + feedback.Val(secret))

			// Confirmed before it is stored, so an app that failed to scan is
			// discovered now rather than at the next sign-in.
			code, err := promptLine("Enter the code your app shows: ")
			if err != nil {
				return err
			}
			if !authz.VerifyTOTP(secret, strings.TrimSpace(code)) {
				return fmt.Errorf("that code does not match, so nothing was changed. Try again and check your phone's clock is right")
			}

			codes, err := accounts.EnableTOTP(args[0], secret)
			if err != nil {
				return err
			}
			auditlog.Record(auditlog.Entry{Action: "users.totp.enabled", Subject: args[0]})
			feedback.Done("the second factor is on for " + feedback.Val(args[0]))
			printRecoveryCodes(codes)
			return nil
		},
	}
}

func newUsersTOTPDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <username>",
		Short: "Turn the second factor off, which is the way back in from a lost phone",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			accounts, err := authz.OpenAccounts()
			if err != nil {
				return err
			}
			if err := accounts.DisableTOTP(args[0]); err != nil {
				return err
			}
			auditlog.Record(auditlog.Entry{Action: "users.totp.disabled", Subject: args[0]})
			feedback.Begin()
			feedback.Done("the second factor is off for " + feedback.Val(args[0]))
			feedback.Note("that account signs in on its password alone now; enrol again with: servlo users totp enable " + args[0])
			return nil
		},
	}
}

func newUsersTOTPCodesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "codes <username>",
		Short: "Issue a fresh set of recovery codes, replacing the old ones",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			accounts, err := authz.OpenAccounts()
			if err != nil {
				return err
			}
			codes, err := accounts.RegenerateRecoveryCodes(args[0])
			if err != nil {
				return err
			}
			auditlog.Record(auditlog.Entry{Action: "users.totp.codes.regenerated", Subject: args[0]})
			feedback.Begin()
			feedback.Done("fresh recovery codes for " + feedback.Val(args[0]))
			printRecoveryCodes(codes)
			return nil
		},
	}
}

// printRecoveryCodes shows a batch once. There is no command to show them
// again, because storing them in a form servlo could reprint would make them a
// second password sitting on the same disk as the first.
func printRecoveryCodes(codes []string) {
	fmt.Println()
	fmt.Println("  Recovery codes. Each works once, in place of a code from the app.")
	fmt.Println("  Write them down now: this is the only time they are shown.")
	fmt.Println()
	for _, code := range codes {
		fmt.Println("    " + code)
	}
	fmt.Println()
}

// promptLine prompts and reads one line, visibly: a TOTP code is not a secret
// worth hiding, and hiding it stops the operator seeing a typo.
func promptLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
