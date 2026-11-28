package providerfields

import (
	"altune/go-api/cmd/providerschema/internal/providerfields/internal/adapters/decodedfields"
	"altune/go-api/cmd/providerschema/internal/providerfields/internal/adapters/schemafile"
	"altune/go-api/cmd/providerschema/internal/providerfields/internal/core/schema"
)

const (
	SeenReference = schema.SeenReference
	SeenLive      = schema.SeenLive
)

type (
	Schema   = schema.Schema
	Entity   = schema.Entity
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
