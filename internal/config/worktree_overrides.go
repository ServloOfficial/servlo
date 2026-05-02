package config

// WorktreePHPVersion returns the worktree's effective PHP version: the
// override from its .lerd.yaml when set, otherwise fallback (the parent's).
// Used by every code path that materialises worktree state on disk.
func WorktreePHPVersion(worktreePath, fallback string) string {
	if cfg, err := LoadProjectConfig(worktreePath); err == nil && cfg != nil && cfg.PHPVersion != "" {
		return cfg.PHPVersion
	}
	return fallback
}

// WorktreeNodeVersion mirrors WorktreePHPVersion for Node. No nginx side
// effects, but the same precedence so `lerd sites` and the dashboard report
// the worktree's effective version rather than the parent's.
func WorktreeNodeVersion(worktreePath, fallback string) string {
	if cfg, err := LoadProjectConfig(worktreePath); err == nil && cfg != nil && cfg.NodeVersion != "" {
		return cfg.NodeVersion
	}
	return fallback
}

// SetWorktreePHPVersion writes the override to the worktree's .lerd.yaml,
// creating the file if missing. Passing "" clears the override. Unlike
// SetProjectPHPVersion this always materialises the file.
func SetWorktreePHPVersion(worktreePath, version string) error {
	cfg, err := LoadProjectConfig(worktreePath)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &ProjectConfig{}
	}
	cfg.PHPVersion = version
	return SaveProjectConfig(worktreePath, cfg)
}

// SetWorktreeNodeVersion mirrors SetWorktreePHPVersion for Node.
func SetWorktreeNodeVersion(worktreePath, version string) error {
	cfg, err := LoadProjectConfig(worktreePath)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &ProjectConfig{}
	}
	cfg.NodeVersion = version
	return SaveProjectConfig(worktreePath, cfg)
}

// WorktreeDBIsolated reports whether the worktree at the given path opted
// into its own database. Returns false when .lerd.yaml is missing or the
// flag is unset.
func WorktreeDBIsolated(worktreePath string) bool {
	if cfg, err := LoadProjectConfig(worktreePath); err == nil && cfg != nil {
		return cfg.DBIsolated
	}
	return false
}

// SetWorktreeDBIsolated writes the flag to the worktree's .lerd.yaml,
// creating the file if missing.
func SetWorktreeDBIsolated(worktreePath string, isolated bool) error {
	cfg, err := LoadProjectConfig(worktreePath)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &ProjectConfig{}
	}
	cfg.DBIsolated = isolated
	return SaveProjectConfig(worktreePath, cfg)
}
