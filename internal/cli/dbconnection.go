package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/serviceops"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewDbConnectionCmd returns the `servlo db:connection` command group: the
// named databases sites can be put on, local or managed.
func NewDbConnectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db:connection",
		Short: "Manage the databases sites can be put on, local or managed",
		Long: `A database is a connection: MySQL or MariaDB running here, PostgreSQL
running here, or a managed database somewhere else. Each site names the
connection its data lives on, and a site that names none uses the default.

A managed database's credentials are read from a prompt rather than taken
as a flag, so they do not end up in the shell history of a server several
people log into.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runDbConnectionList() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the configured connections",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return runDbConnectionList() },
	})
	cmd.AddCommand(newDbConnectionAddCmd())
	cmd.AddCommand(&cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a connection",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return runDbConnectionRemove(args[0]) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "default <name>",
		Short: "Put new sites on this connection",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return runDbConnectionDefault(args[0]) },
	})
	return cmd
}

func runDbConnectionList() error {
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		return err
	}
	if len(reg.Connections) == 0 {
		feedback.Begin()
		feedback.Line("No connections configured, so sites use the local MySQL.")
		feedback.Note("add a managed database with: servlo db:connection add <name> --engine postgres --host <host>")
		return nil
	}
	feedback.Begin()
	for _, c := range reg.Connections {
		where := "managed at " + c.Host
		if c.Local() {
			where = "local service " + c.Service
		}
		marker := " "
		if c.Name == reg.Default {
			marker = "*"
		}
		// Never the password. This prints on a server somebody may be sharing
		// their screen from, and the whole point of the 0600 file is that the
		// credential is not casually visible.
		feedback.Line(fmt.Sprintf("%s %-16s %-9s %s", marker, c.Name, c.Family, where))
	}
	feedback.Note("* is the connection a new site is put on")
	return nil
}

func newDbConnectionAddCmd() *cobra.Command {
	var engine, host, service, user, tlsMode, caCert string
	var port int
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a connection, local or managed",
		Long: `Add a local connection by naming the service it runs on:

    servlo db:connection add primary --service postgres

Or a managed one by naming where it is. The password is prompted for:

    servlo db:connection add managed --engine postgres --host db.example.net --port 25060 --user doadmin

The first connection added becomes the default. Existing sites keep the
database they are already on.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runDbConnectionAdd(args[0], service, engine, host, port, user, tlsMode, caCert)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "A servlo service this connection is (e.g. mysql, postgres, mariadb)")
	cmd.Flags().StringVar(&engine, "engine", "", "Engine of a managed database: mysql or postgres")
	cmd.Flags().StringVar(&host, "host", "", "Host of a managed database")
	cmd.Flags().IntVar(&port, "port", 0, "Port of a managed database (default: the engine's)")
	cmd.Flags().StringVar(&user, "user", "", "Administrative user servlo creates databases as")
	cmd.Flags().StringVar(&tlsMode, "tls", "", "TLS for a managed database: require or verify-ca")
	cmd.Flags().StringVar(&caCert, "ca-cert", "", "Path to the CA certificate, for verify-ca")
	return cmd
}

func runDbConnectionAdd(name, service, engine, host string, port int, user, tlsMode, caCert string) error {
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		return err
	}

	var c dbconn.Connection
	switch {
	case service != "":
		if host != "" || engine != "" {
			return fmt.Errorf("a connection is either a service servlo runs or a database somewhere else, not both: drop --service or drop --host")
		}
		if !serviceops.ServiceInstalled(service) {
			return fmt.Errorf("%s is not installed here, so nothing would answer on it: install it with `servlo service preset %s`", service, service)
		}
		c = dbconn.LocalConnection(name, service)
	case host != "":
		if engine == "" {
			return fmt.Errorf("a managed database needs --engine mysql or --engine postgres, because its dialect decides how servlo talks to it")
		}
		if user == "" {
			return fmt.Errorf("a managed database needs --user, the administrative account servlo creates databases and site users as")
		}
		password, err := readManagedPassword()
		if err != nil {
			return err
		}
		c = dbconn.External(name, engine, host, port, user, password)
		c.TLSMode, c.CACert = tlsMode, caCert
	default:
		return fmt.Errorf("say where this database is: --service <name> for one servlo runs, or --host <host> for a managed one")
	}

	if err := reg.Add(c); err != nil {
		return err
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		return err
	}

	feedback.Begin()
	feedback.Done("added connection " + feedback.Val(name))
	if reg.Default == name {
		feedback.Note("new sites go on it; sites that already exist keep the database they are on")
	}
	if !c.Local() {
		feedback.Note("servlo does not create the database or its user on a managed server yet, so create them there for now")
	}
	return nil
}

// readManagedPassword takes the credential from a prompt rather than a flag, so
// it stays out of the shell history and the process list of a machine that may
// have several people on it.
func readManagedPassword() (string, error) {
	if pw := os.Getenv("SERVLO_DB_PASSWORD"); pw != "" {
		return pw, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("the database password is read from a prompt: run this interactively, or set SERVLO_DB_PASSWORD for an unattended install")
	}
	fmt.Fprint(os.Stderr, "Database password: ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading the password: %w", err)
	}
	if len(strings.TrimSpace(string(pw))) == 0 {
		return "", fmt.Errorf("empty password")
	}
	return string(pw), nil
}

func runDbConnectionRemove(name string) error {
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		return err
	}
	// A connection with sites on it is not something to remove out from under
	// them: their .env still points at that database and the next deploy would
	// rewrite it to somewhere else.
	if sites := sitesOnConnection(name); len(sites) > 0 {
		return fmt.Errorf("%d site(s) are on %q (%s): move them to another connection first",
			len(sites), name, strings.Join(sites, ", "))
	}
	if err := reg.Remove(name); err != nil {
		return err
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		return err
	}
	feedback.Begin()
	feedback.Done("removed connection " + feedback.Val(name))
	if reg.Default != "" {
		feedback.Note("new sites now go on " + reg.Default)
	}
	return nil
}

// sitesOnConnection lists the sites that name this connection, by domain.
func sitesOnConnection(name string) []string {
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	var on []string
	for _, s := range reg.Sites {
		if s.Database == name {
			on = append(on, s.PrimaryDomain())
		}
	}
	return on
}

func runDbConnectionDefault(name string) error {
	reg, err := dbconn.LoadRegistry()
	if err != nil {
		return err
	}
	if err := reg.SetDefault(name); err != nil {
		return err
	}
	if err := dbconn.SaveRegistry(reg); err != nil {
		return err
	}
	feedback.Begin()
	feedback.Done("new sites go on " + feedback.Val(name))
	feedback.Note("sites that already exist keep the database they are on")
	return nil
}
