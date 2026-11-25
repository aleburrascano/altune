package schemafile

import (
	"altune/go-api/internal/shared/providerschema/internal/core/schema"
	"bytes"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

func Load(path string) (schema.Schema, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return schema.Schema{}, fmt.Errorf("read schema: %w", err)
	}
	var s schema.Schema
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&s); err != nil {
		return schema.Schema{}, fmt.Errorf("parse schema %s: %w", path, err)
	}
	for name, entity := range s.Entities {
		if entity == nil {
			return schema.Schema{}, fmt.Errorf("parse schema %s: entity %s is empty", path, name)
		}
	}
	return s, nil
}

func Save(path string, s schema.Schema) error {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(s.Sorted()); err != nil {
		return fmt.Errorf("encode schema: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
