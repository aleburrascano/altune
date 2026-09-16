# DNS — the `duckdns` CLI

Staging DNS lives on DuckDNS (`altune-staging.duckdns.org`). The wrapper that drives it
is versioned at **`services/go-api/deploy/duckdns`** — a minimal CLI over the DuckDNS
HTTP API.

## Commands

```
duckdns update <domain> [ip]   # update the A record; ip omitted = duckdns auto-detects caller IP
duckdns ip <domain>            # show the currently resolved IP for <domain>.duckdns.org
duckdns txt <domain> <value>   # set the TXT record (e.g. an ACME/DNS-01 challenge)
duckdns txt-clear <domain>     # clear the TXT record
duckdns set-token <token>      # save a token to ~/.config/duckdns/token (chmod 600)
duckdns help
```

`<domain>` accepts either the bare label (`altune-staging`) or the FQDN
(`altune-staging.duckdns.org`).

## Token resolution

Checked in order, first hit wins:

1. `--token <t>` on the command line
2. `$DUCKDNS_TOKEN`
3. `~/.config/duckdns/token` (override the path with `$DUCKDNS_TOKEN_FILE`)

Never commit the token. `duckdns set-token` writes it to the token file at mode 600.

## Limitation — cannot create subdomains

DuckDNS has no API to **create** a subdomain. New subdomains are added on the
duckdns.org website while logged in. This CLI drives only what an existing token allows:
updating A records, setting/clearing TXT records, and checking status.
