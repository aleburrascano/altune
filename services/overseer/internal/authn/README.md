`authn` decides which bearer tokens belong to the owner.

- `New` builds a verifier for the owner's Supabase tokens: ES256 against the JWKS, and HS256 only when a secret is configured.
- It refuses an expired token, a bad signature, `alg: none`, an unknown key id, a missing subject and a foreign or absent issuer or audience.
- The verifier itself lives in `internal/adapters/verifier`.
