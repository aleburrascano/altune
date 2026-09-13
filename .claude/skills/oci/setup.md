# oci — install, auth, config

Reference for getting `oci` working and picking the right identity. Reached from `SKILL.md` step 1 when auth is missing or a config decision comes up. Config is the part the user most wants off the console, so it lives here in full.

## Install

Windows (PowerShell):
```powershell
iex ((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/oracle/oci-cli/master/scripts/install/install.ps1'))
```
Linux / macOS:
```bash
bash -c "$(curl -L https://raw.githubusercontent.com/oracle/oci-cli/master/scripts/install/install.sh)"
```
macOS (Homebrew): `brew update && brew install oci-cli`. PyPI package is `oci-cli` (`pip install oci-cli`) but Oracle prefers the install script or a virtualenv.

Confirm: `oci --version`.

## The config file

Lives at `~/.oci/config` (Windows: `%HOMEDRIVE%%HOMEPATH%\.oci\config`). One or more profile sections; the first is `[DEFAULT]`, extra ones are `[NAMED]`. Each profile holds:

| field | what |
|-------|------|
| `user` | user OCID |
| `fingerprint` | fingerprint of the uploaded public key, `12:34:...:ef` |
| `tenancy` | tenancy OCID (this is also the **root compartment** OCID) |
| `region` | e.g. `us-ashburn-1` |
| `key_file` | path to the private `.pem` |
| `pass_phrase` | only if the private key is encrypted |

## Set up API-key auth (the standard path)

```bash
oci setup config
```
Interactive. Asks for config location (default `~/.oci/config`), user OCID, tenancy OCID, region, and whether to make a new RSA key pair or point at an existing one. On a new key it writes the private key, public key, and computes the `fingerprint`, filling in `[DEFAULT]`.

**The public key must be added in the console before calls work:** Console > Profile menu > **User settings** > **Token and keys** > **Add API Key** > paste/upload the public key > **Add**. The console then shows the fingerprint — it must match the config.

Fix key/config permissions if `oci` complains they are too open:
```bash
oci setup repair-file-permissions --file ~/.oci/config
```
Generate a key pair on its own: `oci setup keys`.

## Session (browser) auth — good for humans, expires

```bash
oci session authenticate                    # browser login, makes a security-token profile
oci session validate --profile <name> --auth security_token
oci session refresh  --profile <name> --auth security_token
```
Then add `--auth security_token` to commands (or `export OCI_CLI_AUTH=security_token`). Browser sessions last ~1 hour, refreshable up to 24 hours. `--no-browser` uses API-key style; persistence 5–60 min via `--session-expiration-in-minutes`.

## Instance / resource principal — no config file

For code running inside OCI (a VM, a function). Needs an IAM dynamic group + policy set up console-side first, then:
```bash
oci <cmd> --auth instance_principal
oci <cmd> --auth resource_principal
```
`--auth` values: `api_key` (default), `security_token`, `instance_principal`, `resource_principal`, `instance_obo_user`, `oke_workload_identity`.

## Picking a profile

Precedence, high to low: `--profile <name>` > `OCI_CLI_PROFILE` env var > `default_profile` in `~/.oci/oci_cli_rc` `[OCI_CLI_SETTINGS]`.

Useful env vars: `OCI_CLI_PROFILE`, `OCI_CLI_AUTH`, `OCI_CLI_CONFIG_FILE`, `OCI_CLI_REGION`, `OCI_CLI_COMPARTMENT_ID`.

## Set a default compartment (skip typing --compartment-id)

No built-in default — the tenancy OCID is the root compartment. Set one in `~/.oci/oci_cli_rc`:
```ini
[DEFAULT]
compartment-id = ocid1.compartment.oc1..aaaa...
```
or `export OCI_CLI_COMPARTMENT_ID=ocid1.compartment...`.

## Verify it works

```bash
oci iam region list --output table          # cheap read, proves auth
oci iam compartment list --all --query "data[].{name:name,id:id}" --output table
```
