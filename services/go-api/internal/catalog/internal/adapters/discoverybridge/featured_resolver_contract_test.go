package discoverybridge_test

import (
	"altune/go-api/internal/catalog/internal/adapters/contract"
	"testing"
)

func TestFeaturedResolver_Contract(t *testing.T) {
	contract.FeaturedResolverContract(t, resolverOverCatalogCredits)
}
