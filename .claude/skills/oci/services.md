# oci — command reference by domain

Exact `oci` commands per domain, verified against Oracle's official cmdref (~v3.92). Reached from `SKILL.md` step 2 to find the command for a resource. Always confirm required params live with `oci <service> <resource> <action> --help` before running — this is a fast index, help is the source of truth. `$C` below is a compartment OCID.

Complex params (any `*-rules`, `*-details`, `shape-config`, `destinations`) take JSON — generate a template with `--generate-param-json-input <param>` and pass with `file://path.json` rather than hand-quoting.

## IAM & compartments

| command | required |
|---|---|
| `oci iam compartment create` | `-c` (parent), `--name`, `--description` |
| `oci iam compartment list` | none (`--compartment-id-in-subtree`, `--lifecycle-state` filters) |
| `oci iam compartment get/update/delete` | `--compartment-id` |
| `oci iam compartment recover` | `--compartment-id` (delete is soft, recoverable) |
| `oci iam region list` | none — lists all regions |
| `oci iam availability-domain list` | none (`-c` optional, defaults to tenancy) |
| `oci iam user create` | `--name`, `--description` |
| `oci iam group create` / `add-user` | `--name`,`--description` / `--group-id`,`--user-id` |
| `oci iam policy create` | `-c`, `--name`, `--description`, `--statements` (JSON array) |
| `oci iam policy list` | `-c` |

```bash
oci iam compartment create -c ocid1.tenancy.oc1..ROOT --name altune-prod --description "Altune prod"
oci iam policy create -c $C --name altune-admins --description "Admin access" \
  --statements '["Allow group Admins to manage all-resources in compartment altune-prod"]'
```
Compartment names are unique in the tenancy; delete needs it empty and runs async.

## Compute

| command | required |
|---|---|
| `oci compute instance launch` | `--availability-domain`, `-c`, `--subnet-id` (`--shape` needed in practice) |
| `oci compute instance list` | `-c` |
| `oci compute instance get` | `--instance-id` |
| `oci compute instance action` | `--instance-id`, `--action` |
| `oci compute instance list-vnics` | `--instance-id` (this is where IPs come from) |
| `oci compute instance terminate` | `--instance-id` (`--preserve-boot-volume`, `--force`) |
| `oci compute image list` | `-c` (`--operating-system`, `--operating-system-version`) |
| `oci compute shape list` | `-c` |

`--action`: `STOP, START, SOFTRESET, RESET, SOFTSTOP, SENDDIAGNOSTICINTERRUPT, DIAGNOSTICREBOOT, REBOOTMIGRATE`. Waiter states: `RUNNING, STOPPED, PROVISIONING, TERMINATED`.

```bash
oci compute instance launch \
  --availability-domain "Uocm:EU-FRANKFURT-1-AD-1" -c $C \
  --shape "VM.Standard.E4.Flex" --shape-config '{"ocpus": 2, "memoryInGBs": 32}' \
  --image-id "$IMAGE" --subnet-id "$SUBNET" \
  --assign-public-ip true --display-name "altune-e4-demo" \
  --ssh-authorized-keys-file ~/.ssh/id_rsa.pub \
  --wait-for-state RUNNING
```
**Flex shapes (`.Flex`) require `--shape-config`** with `ocpus` + `memoryInGBs`. `--image-id` and `--source-details` are mutually exclusive. Get the instance's IP from `list-vnics`, not `get`. For `--shape-config`/`--source-details`, generate the JSON with `--generate-param-json-input`; exact field names aren't in the cmdref.

## Networking (VCN)

| command | required |
|---|---|
| `oci network vcn create` | `-c` (`--cidr-blocks` JSON array; `--cidr-block` deprecated) |
| `oci network subnet create` | `-c`, `--vcn-id` |
| `oci network internet-gateway create` | `-c`, `--is-enabled`, `--vcn-id` |
| `oci network route-table create` | `-c`, `--vcn-id`, `--route-rules` |
| `oci network route-table update` | `--rt-id` (`--route-rules` **replaces** the whole set) |
| `oci network security-list create` | `-c`, `--vcn-id`, `--ingress-security-rules`, `--egress-security-rules` (pass `'[]'` for an empty side) |
| `oci network nsg create` | `-c`, `--vcn-id` |
| `oci network nsg rules add` | `--nsg-id`, `--security-rules` (appends, ≤25/call) |

Route rule: `{"cidrBlock":"0.0.0.0/0","networkEntityId":"<igw-ocid>"}`. Ingress rule: `{"protocol":"6","source":"0.0.0.0/0","sourceType":"CIDR_BLOCK","isStateless":false,"tcpOptions":{"destinationPortRange":{"min":22,"max":22}}}` (protocol `6`=TCP, `17`=UDP, `1`=ICMP, `all`). NSG rules add a `"direction":"INGRESS|EGRESS"` field and can reference another NSG; security lists cannot.

Public subnet with SSH, end to end:
```bash
VCN=$(oci network vcn create -c $C --cidr-blocks '["10.0.0.0/16"]' --display-name altune-vcn --query 'data.id' --raw-output)
IGW=$(oci network internet-gateway create -c $C --vcn-id $VCN --is-enabled true --query 'data.id' --raw-output)
RT=$(oci network route-table create -c $C --vcn-id $VCN \
  --route-rules "[{\"cidrBlock\":\"0.0.0.0/0\",\"networkEntityId\":\"$IGW\"}]" --query 'data.id' --raw-output)
SL=$(oci network security-list create -c $C --vcn-id $VCN \
  --ingress-security-rules '[{"protocol":"6","source":"0.0.0.0/0","sourceType":"CIDR_BLOCK","isStateless":false,"tcpOptions":{"destinationPortRange":{"min":22,"max":22}}}]' \
  --egress-security-rules  '[{"protocol":"all","destination":"0.0.0.0/0","destinationType":"CIDR_BLOCK","isStateless":false}]' --query 'data.id' --raw-output)
SUB=$(oci network subnet create -c $C --vcn-id $VCN --cidr-block 10.0.1.0/24 \
  --route-table-id $RT --security-list-ids "[\"$SL\"]" --query 'data.id' --raw-output)
```
`update` replaces the whole rule set; `nsg rules add` appends. Egress uses `destination`/`destinationType`, ingress uses `source`/`sourceType`.

## Storage

Object Storage:
| command | required |
|---|---|
| `oci os ns get` | none (the namespace) |
| `oci os bucket create` | `-c`, `--name` |
| `oci os bucket list` | `-c` |
| `oci os object put` | `--bucket-name/-bn`, `--file` |
| `oci os object get` | `-bn`, `--name`, `--file` |
| `oci os object list` | `-bn` |
| `oci os object bulk-upload` / `bulk-download` | `-bn`, `--src-dir` / `-bn`, `--dest-dir` |

Block Volume:
| command | required |
|---|---|
| `oci bv volume create` | `--availability-domain`, `-c` (**set `--size-in-gbs`; default is 1 TB**) |
| `oci compute volume-attachment attach` | `--instance-id`, `--type`, `--volume-id` |

`attach --type`: `iscsi, paravirtualized, emulated, service_determined`. Bucket `--public-access-type`: `NoPublicAccess` (default), `ObjectRead`, `ObjectReadWithoutList`.

```bash
oci os bucket create -c $C --name altune-media
oci os object put --bucket-name altune-media --file ./track01.flac --force
```
Namespace auto-detects if `--namespace-name` omitted. bulk-upload/download **prompt on every collision** — always pass `--overwrite` or `--no-overwrite` in scripts. `paravirtualized` volumes auto-attach; `iscsi` needs in-guest config.

## Database (Autonomous)

`oci db autonomous-database create` — only `-c` and `--db-name` are hard-required; these matter in practice:
- Size: `--compute-model ECPU --compute-count N` (ECPU recommended; legacy `--cpu-core-count` is mutually exclusive with those).
- Storage: `--data-storage-size-in-tbs` (serverless).
- `--admin-password` (or `--secret-id` from Vault).
- `--db-workload`: `OLTP` (default), `DW`, `AJD`, `APEX`, `LH`.
- `--is-free-tier` for Always Free (1 CPU / 20 GB).

| command | required |
|---|---|
| `oci db autonomous-database list` | `-c` |
| `oci db autonomous-database get/start/stop/update/delete` | `--autonomous-database-id` |

```bash
oci db autonomous-database create -c $C --db-name ATPPROD1 --db-workload OLTP \
  --compute-model ECPU --compute-count 2 --data-storage-size-in-tbs 1 \
  --admin-password 'Str0ngP4ssw0rd!' --wait-for-state AVAILABLE
```
**Admin password: 12–30 chars, at least one upper, one lower, one digit, no `"`, cannot contain "admin".** `--db-name`: starts with a letter, ≤30 alphanumeric, unique in tenancy. Waiter: create/start → `AVAILABLE`, stop → `STOPPED`.

## Monitoring & observability

`oci monitoring alarm create` — required: `-c`, `--destinations` (JSON array of topic OCIDs), `--display-name`, `--is-enabled`, `--metric-compartment-id`, `--namespace`, `--query-text` (MQL), `--severity`.

The MQL field is **`--query-text`**, not the global `--query`.

| command | required |
|---|---|
| `oci monitoring alarm list` | `-c` |
| `oci monitoring metric-data summarize-metrics-data` | `-c`, `--namespace`, `--query-text` |
| `oci monitoring metric list` | `-c` |
| `oci logging log-group create` | `-c`, `--display-name` |
| `oci logging log create` | `--log-group-id`, `--display-name`, `--log-type` (`CUSTOM`\|`SERVICE`) |
| `oci logging-search search-logs` | `--search-query`, `--time-start`, `--time-end` |
| `oci ons topic create` | `-c`, `--name` (alarms fire to a Notifications topic) |

MQL example: `CpuUtilization[1m].mean() > 90` or `.absent()`. Times are RFC 3339 (`2026-09-13T00:00:00Z`); metric-data defaults to the last 3 hours.

```bash
# make a topic first, put its OCID in destinations.json as ["ocid1.onstopic.oc1..TOPIC"]
oci monitoring alarm create -c $C --metric-compartment-id $C \
  --namespace oci_computeagent --display-name "High CPU" \
  --query-text "CpuUtilization[1m].mean() > 90" \
  --severity CRITICAL --is-enabled true --destinations file://destinations.json
```
An alarm needs a Notifications topic (`oci ons topic create`) to fire to. Severity values seen: `CRITICAL`, `ERROR`, `WARNING`, `INFO`.
