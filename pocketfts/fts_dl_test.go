package pocketfts

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

	if err := loadFTSLibrary(dbPath); err != nil {
		t.Fatalf("failed to load embedded ftscore library: %v", err)
	}

	setCallTimeout(15000)
}
