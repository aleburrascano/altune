package providerfields

import (
	"altune/go-api/internal/shared/providerfields/internal/adapters/decodedfields"
	"altune/go-api/internal/shared/providerfields/internal/adapters/schemafile"
	"altune/go-api/internal/shared/providerfields/internal/core/schema"
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
