package ui

import (
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/presetfixtures"
)

// Add-ons ship in the external store, not the binary. Tests resolve them through
// the config seam's extra layer so add-on-shaped mechanism and functionality
// tests keep running without a network fetch.
func init() { config.SetExtraPresetsForTest(presetfixtures.FS()) }
