package main

import (
	"path/filepath"
	"testing"
)

// TestSetCallTimeoutBindingLoads verifies that the FtsSetCallTimeout symbol
// exported by the embedded ftscore library is resolved and callable. It does
// not verify the timeout behavior itself (that's ftscore's responsibility),
// only that pocket-fts's dlsym binding for the new symbol is wired up
// correctly after the upgrade.
func TestSetCallTimeoutBindingLoads(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	if err := LoadFTSLibrary(dbPath); err != nil {
		t.Fatalf("failed to load embedded ftscore library: %v", err)
	}
	defer func() {
		if err := UnloadFTSLibrary(); err != nil {
			t.Errorf("failed to unload ftscore library: %v", err)
		}
	}()

	SetCallTimeout(15000)
}
