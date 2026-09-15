---
name: oci
description: Drive the Oracle Cloud Infrastructure CLI (oci) to provision, configure, and monitor cloud resources from the terminal instead of the web console.
disable-model-invocation: true
---

Operate Oracle Cloud through the `oci` CLI so the user never opens the console. One job, three shapes it takes: **provision** (create resources), **configure** (change settings on existing ones), **monitor** (read state, metrics, alarms). Every task runs the same loop below.

The command shape is always `oci <service> <resource> <action> [params]`, e.g. `oci compute instance list --compartment-id <ocid>`.

Two reference files, reached only when their branch fires:
- **setup.md** — install, the config file, API-key / session / instance-principal auth, profiles, default compartment. Go here whenever a call fails on auth or the user asks about credentials or configuration of `oci` itself.
- **services.md** — exact command reference per domain (IAM, compute, networking, storage, database, monitoring), with worked examples. Go here to find the command for a resource.

## 1. Confirm you can call the API

Run one cheap read: `oci iam region list --output table`. If it errors on auth or config, stop and fix it via **setup.md** before anything else — a create against a broken profile wastes work. Also settle which **profile** (`--profile`) and **compartment** (`--compartment-id`) this task targets; the tenancy OCID is the root compartment.

Done when: a read command returns real data under the intended profile and compartment.

## 2. Find the exact command

Reach for it in this order: **services.md** for the common domains, else discover live with `oci <service> --help`, `oci <service> <resource> --help`, `oci <service> <resource> <action> --help`. The help output is the source of truth for required params.

Done when: you have the full command and know every required parameter and which OCIDs it needs.

## 3. Read before you write

Never invent an OCID. List or get the things the command references (compartment, subnet, image, shape, existing resource) and pull their OCIDs with `--query`, e.g. `oci compute image list --compartment-id <ocid> --query "data[0].id" --raw-output`. This also shows current state before you change it.

Done when: every OCID and value the command needs is a real value you fetched, not a placeholder.

## 4. Build the command — use JSON input for anything non-trivial

For a command with more than a few params, or any configuration change, generate the template instead of hand-typing:
```bash
oci <service> <resource> <action> --generate-full-command-json-input > in.json
# edit in.json
oci <service> <resource> <action> --from-json file://in.json
```
This is the reliable way to do rich configuration and to repeat it. There is no `--dry-run`; `--generate-full-command-json-input` and `--render`-style preview do not run anything, so use them to inspect before you commit.

Done when: the command (or `in.json`) is complete with real values and you have looked it over.

## 5. Run it — block until the resource is actually ready

Add a waiter to creates and updates so the call returns only when the resource reaches its state:
```bash
--wait-for-state ACTIVE --max-wait-seconds 900
```
(states vary per resource; `RUNNING` for instances, `AVAILABLE` for databases, etc.) Exit codes: `0` ok, `2` waiter timed out, `1` other error.

**Stop and confirm with the user first** for anything that is destructive, outward-facing, or costs money: `delete`/`terminate`, and creating billable resources (compute, databases, load balancers). Use `--force` only after they have said yes. Reads and state checks never need confirmation.

Done when: the command exits `0` (or the user has approved a destructive/billable action that then exits `0`).

## 6. Verify and report

Confirm the real state, don't trust the create call alone:
```bash
oci <service> <resource> get --<resource>-id <ocid> --query "data.\"lifecycle-state\"" --raw-output
```
Then tell the user what now exists or changed, its OCID, and its state.

Done when: a fresh `get` shows the intended state and you have reported the outcome.
