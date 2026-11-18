package providerschema

import (
	"altune/go-api/internal/shared/providerschema/internal/adapters/decodedfields"
	"altune/go-api/internal/shared/providerschema/internal/adapters/schemafile"
	"altune/go-api/internal/shared/providerschema/internal/core/schema"
)

const (
	SeenReference = schema.SeenReference
	SeenLive      = schema.SeenLive
	TypeUnseen    = schema.TypeUnseen
)

type (
	Schema   = schema.Schema
	Entity   = schema.Entity
	Fixture  = schema.Fixture
	Field    = schema.Field
	Observed = schema.Observed
	Reads    = schema.Reads
)

var (
	Observe       = schema.Observe
	Load          = schemafile.Load
	Save          = schemafile.Save
	DecodedFields = decodedfields.Read
)
