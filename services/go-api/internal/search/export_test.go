package search

import "altune/go-api/internal/search/internal/ports"

func UseIdentityStore(d *Deps, store ports.IdentityStore) { d.identityStore = store }
