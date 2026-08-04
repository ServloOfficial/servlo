package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/spf13/cobra"
)

// NewMinioMigrateCmd returns the minio:migrate command.
func NewMinioMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "minio:migrate",
		Short: "Migrate MinIO data to RustFS",
		Long: `Migrates an existing MinIO installation to RustFS.

This command will:
  - Stop the servlo-minio service
  - Copy data from ~/.local/share/servlo/data/minio to ~/.local/share/servlo/data/rustfs
  - Start the servlo-rustfs service

RustFS uses the same S3 API and credentials as MinIO, so no application changes are needed.`,
		RunE: runMinioMigrate,
	}
}

func runMinioMigrate(_ *cobra.Command, _ []string) error {
	minioDataDir := config.DataSubDir("minio")
	rustfsDataDir := config.DataSubDir("rustfs")

	// Check that there is something to migrate.
	if _, err := os.Stat(minioDataDir); os.IsNotExist(err) {
		return fmt.Errorf("no MinIO data found at %s\nNothing to migrate", minioDataDir)
	}

	feedback.Begin()
	feedback.Line("migrating MinIO data to RustFS")

	// Stop minio if running.
	status, _ := podman.UnitStatus("servlo-minio")
	if status == "active" || status == "activating" {
		st := feedback.Start("stopping servlo-minio")
		if err := podman.StopUnit("servlo-minio"); err != nil {
			st.Fail(err)
			return fmt.Errorf("could not stop servlo-minio: %w", err)
		}
		st.OK("")
	}

	// Remove minio quadlet so it no longer auto-starts.
	rm := feedback.Start("removing MinIO quadlet")
	if err := podman.RemoveQuadlet("servlo-minio"); err != nil && !os.IsNotExist(err) {
		rm.Fail(err)
	} else {
		rm.OK("")
	}
	if err := podman.DaemonReloadFn(); err != nil {
		fmt.Printf("  warn: daemon-reload failed: %v\n", err)
	}

	// Copy minio data to rustfs data dir.
	cpStep := feedback.Start("copying data directory")
	if err := os.MkdirAll(rustfsDataDir, 0755); err != nil {
		cpStep.Fail(err)
		return fmt.Errorf("creating rustfs data dir: %w", err)
	}
	cp := exec.Command("cp", "-a", minioDataDir+"/.", rustfsDataDir+"/")
	if out, err := cp.CombinedOutput(); err != nil {
		cpStep.Fail(fmt.Errorf("%s", out))
		return fmt.Errorf("copying data: %s", out)
	}
	cpStep.OK("")

	// Update global config: disable minio entry if present, ensure rustfs exists.
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.Services == nil {
		cfg.Services = map[string]config.ServiceConfig{}
	}
	minioWasEnabled := cfg.Services["minio"].Enabled
	delete(cfg.Services, "minio")
	if _, hasRustfs := cfg.Services["rustfs"]; !hasRustfs {
		cfg.Services["rustfs"] = config.ServiceConfig{
			Enabled: minioWasEnabled,
			Image:   "docker.io/rustfs/rustfs:latest",
			Port:    9000,
		}
	}
	if err := config.SaveGlobal(cfg); err != nil {
		fmt.Printf("  warn: could not save config: %v\n", err)
	}

	// Install and start RustFS.
	inst := feedback.Start("installing RustFS")
	if err := ensureServiceQuadlet("rustfs"); err != nil {
		inst.Fail(err)
		return fmt.Errorf("installing rustfs quadlet: %w", err)
	}
	inst.OK("")

	startStep := feedback.Start("starting servlo-rustfs")
	if err := podman.StartUnit("servlo-rustfs"); err != nil {
		startStep.Fail(err)
		return fmt.Errorf("starting servlo-rustfs: %w", err)
	}
	_ = config.SetServiceManuallyStarted("rustfs", true)
	startStep.OK("")

	feedback.Done("migration complete")
	feedback.Note("RustFS console: http://localhost:9001 · credentials servlo / servlopassword")
	feedback.Note("MinIO data left at " + minioDataDir + " — delete once verified: rm -rf " + minioDataDir)
	return nil
}
