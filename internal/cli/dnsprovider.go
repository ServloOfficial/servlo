package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realrashid/servlo/internal/certs"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dnsprovider"
	"github.com/realrashid/servlo/internal/feedback"
)

// NewDNSProviderCmd returns the dns-provider command group. Wildcard
// certificates need DNS-01, DNS-01 needs a credential that can rewrite the
// zone, and an operator should not have to hand-edit a YAML file to supply the
// most dangerous secret servlo holds.
func NewDNSProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dns-provider",
		Short: "Manage the DNS credentials wildcard certificates need",
		Long: `Wildcard certificates are proved with a DNS-01 challenge, which publishes a TXT
record in the domain's zone. That needs an API credential for the DNS provider.

The credential is stored in ` + "`" + `dns-providers.yaml` + "`" + ` in servlo's config directory,
readable only by the user servlo runs as, and never inside a site directory.

Scope the token as narrowly as the provider allows. Servlo only ever creates and
deletes TXT records under _acme-challenge, so a token limited to DNS edits for
the specific zone is enough.`,
	}
	cmd.AddCommand(newDNSProviderSetCmd(), newDNSProviderListCmd(), newDNSProviderRemoveCmd(), NewDNSProviderUseCmd())
	return cmd
}

func newDNSProviderSetCmd() *cobra.Command {
	var token, keyID, secret, region string
	cmd := &cobra.Command{
		Use:   "set <cloudflare|digitalocean|route53>",
		Short: "Record the credentials for a DNS provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			creds := dnsprovider.Credentials{
				Provider:        dnsprovider.Name(args[0]),
				APIToken:        token,
				AccessKeyID:     keyID,
				SecretAccessKey: secret,
				Region:          region,
			}
			if err := dnsprovider.SaveCredentials(creds); err != nil {
				return err
			}
			feedback.Begin()
			feedback.Done("stored credentials for " + creds.String())
			feedback.Note("turn on DNS-01 with: servlo dns-provider use " + args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "API token (cloudflare, digitalocean)")
	cmd.Flags().StringVar(&keyID, "access-key-id", "", "Access key ID (route53)")
	cmd.Flags().StringVar(&secret, "secret-access-key", "", "Secret access key (route53)")
	cmd.Flags().StringVar(&region, "region", "", "Region recorded for reference (route53)")
	return cmd
}

func newDNSProviderListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show which DNS providers have credentials",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			configured := dnsprovider.Configured()
			feedback.Begin()
			if len(configured) == 0 {
				feedback.Note("no DNS provider configured; wildcard certificates need one")
				return nil
			}
			for _, name := range configured {
				creds, err := dnsprovider.LoadCredentials(name)
				if err != nil {
					feedback.Warn("%s: %v", name, err)
					continue
				}
				// String() redacts. The secret never reaches the terminal,
				// where it would land in scrollback and shell history captures.
				feedback.Line(creds.String())
			}
			return nil
		},
	}
}

func newDNSProviderRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <provider>",
		Short: "Forget a DNS provider's credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := dnsprovider.RemoveCredentials(dnsprovider.Name(args[0])); err != nil {
				return err
			}
			feedback.Begin()
			feedback.Done("removed credentials for " + args[0])
			return nil
		},
	}
}

// NewDNSProviderUseCmd is the switch that puts DNS-01 in force. Separate from
// storing a credential because holding one and using it are different
// decisions: an operator may configure a provider for one wildcard site and
// leave everything else on HTTP-01, which needs no credential at all.
func NewDNSProviderUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <provider|http-01>",
		Short: "Choose how certificates prove control of a domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			feedback.Begin()

			if strings.EqualFold(args[0], "http-01") {
				cfg.Certs.Challenge = ""
				cfg.Certs.DNSProvider = ""
				if err := config.SaveGlobal(cfg); err != nil {
					return err
				}
				feedback.Done("certificates will be proved over http-01")
				return nil
			}

			name := dnsprovider.Name(args[0])
			// Checked before it is recorded: a challenge pointing at a provider
			// with no credentials would refuse every issuance later, including
			// the renewals of sites that were working.
			if _, err := dnsprovider.LoadCredentials(name); err != nil {
				return fmt.Errorf("%w\n\nStore them first: servlo dns-provider set %s --token …", err, name)
			}
			cfg.Certs.Challenge = string(certs.ChallengeDNS01)
			cfg.Certs.DNSProvider = string(name)
			if err := config.SaveGlobal(cfg); err != nil {
				return err
			}
			feedback.Done("certificates will be proved over dns-01 using " + string(name))
			feedback.Note("wildcard domains can now be secured")
			return nil
		},
	}
}
