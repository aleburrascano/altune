package schemafile_test

import (
	"altune/go-api/cmd/providerschema/internal/providerfields/internal/adapters/schemafile"
	"altune/go-api/cmd/providerschema/internal/providerfields/internal/core/schema"
	"path/filepath"
	"strings"
	"testing"
)

func TestSave_WritesAFileLoadReadsBackAndNamesThePathItCannotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.yaml")

	if err := schemafile.Save(path, schema.Schema{}); err != nil {
		t.Fatalf("Save = %v", err)
	}
	if _, err := schemafile.Load(path); err != nil {
		t.Fatalf("Load after Save = %v", err)
	}
	missing := filepath.Join(dir, "no-such-dir", "schema.yaml")
	if err := schemafile.Save(missing, schema.Schema{}); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("Save into a missing directory = %v, want an error naming %s", err, missing)
	}
}
