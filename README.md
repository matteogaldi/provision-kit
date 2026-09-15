# ProvisionKit

ProvisionKit runs YAML workflows of HTTP calls as a DAG. Each step is a request to another service; later steps can use earlier JSON responses. Use it to orchestrate multi-service provisioning — create an account, then a network, then a VM — without writing a custom runner for each flow.

You can validate and execute a workflow from the CLI, or start an HTTP server that accepts provision requests and tracks each run as an operation.

## Install

Requires Go (see `go.mod` for the version).

```bash
go build -o provisionkit ./cmd/provisionkit
```

Run tests:

```bash
go test ./...
```

## Quick start

Validate a workflow, print its execution order, then run it locally:

```bash
./provisionkit validate examples/create-vm.yaml
./provisionkit graph examples/create-vm.yaml
./provisionkit run examples/create-vm.yaml \
  --input customer_id=acme \
  --input plan=standard
```

`run` executes in-process, prints the operation as JSON, and exits non-zero if any step fails.

To expose the same workflows over HTTP:

```bash
./provisionkit serve --workflows examples --addr :8080
```

Then start a run by workflow name:

```bash
curl -sS -X POST http://127.0.0.1:8080/v1/provision/create-vm \
  -H 'Content-Type: application/json' \
  -d '{"inputs":{"customer_id":"acme","plan":"standard"}}'
```

The server responds with `202 Accepted` and an `operation_id`. Poll status with:

```bash
./provisionkit inspect op_0123456789abcdef
# or
curl -sS http://127.0.0.1:8080/v1/operations/op_0123456789abcdef
```

`run --server` does the same POST + poll loop for you:

```bash
./provisionkit run create-vm --server http://127.0.0.1:8080 \
  --input customer_id=acme \
  --input plan=standard
```

## Workflows

A workflow is a YAML file. `config.yaml` and `examples/create-vm.yaml` are complete examples.

```yaml
version: "1"
name: create-vm

services:
  cloudstack: "https://cloud.example.test"
  dns: "https://dns.example.test"

inputs:
  customer_id:
    type: string
  plan:
    type: string

steps:
  - id: create_network
    request:
      method: POST
      url: "{{ services.cloudstack }}/networks"
      body:
        customerId: "{{ inputs.customer_id }}"

  - id: create_vm
    depends_on: [create_network]
    request:
      method: POST
      url: "{{ services.cloudstack }}/vms"
      body:
        networkId: "{{ steps.create_network.output.id }}"
        plan: "{{ inputs.plan }}"
    retry:
      attempts: 3
      backoff: exponential

  - id: configure_dns
    depends_on: [create_vm]
    request:
      method: POST
      url: "{{ services.dns }}/records"
      body:
        ip: "{{ steps.create_vm.output.ip }}"
```

### Fields

| Field | Description |
| --- | --- |
| `version` | Must be `"1"`. |
| `name` | Workflow identifier (`[A-Za-z0-9][A-Za-z0-9_-]*`). Used as the URL name under `/v1/provision/{name}`. |
| `services` | Named base URLs referenced as `{{ services.<name> }}`. |
| `inputs` | Declared run-time values. Currently only `type: string`. Required unless `required: false`. |
| `steps` | HTTP calls. At least one is required. IDs must be unique. |

### Steps

Each step sends one HTTP request (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, or `HEAD`).

- `depends_on` lists steps that must succeed first. Independent steps run in parallel (up to `--concurrency`, default 8).
- A 2xx response succeeds the step. JSON bodies become `output`; empty or non-JSON bodies leave `output` unset.
- A non-2xx response or transport error fails the attempt.

Optional `retry`:

```yaml
retry:
  attempts: 5
  backoff: exponential   # none | constant | exponential
  delay: 200ms           # optional; default 100ms, exponential cap 2s
```

Attempts start at 1. `none` does not wait between tries. `constant` waits `delay` each time. `exponential` doubles from `delay` up to 2 seconds.

### Interpolation

`{{ ... }}` expressions work in `request.url`, `request.headers`, and `request.body`:

| Expression | Resolves to |
| --- | --- |
| `{{ inputs.customer_id }}` | The input value for this run |
| `{{ services.cloudstack }}` | The service base URL |
| `{{ steps.create_vm.output.ip }}` | A field from a prior step’s JSON body |

A step may only reference outputs of steps it depends on (directly or transitively). Validation catches unknown names, cycles, and illegal references.

## CLI

```
provisionkit <command> [arguments]

Commands:
  validate <file>              Validate a workflow definition
  graph <file>                 Print the execution DAG
  serve [flags]                Start the API server
  run <file-or-name> [flags]   Run a workflow
  inspect <operation-id>       Inspect an operation

Serve flags:
  --workflows <path>   Workflow file or directory (required)
  --addr <addr>        Listen address (default :8080)
  --db <path>          SQLite database path (default: in-memory)
  --concurrency <n>    Max parallel ready steps (default 8)

Run flags:
  --input key=value    Workflow input (repeatable)
  --server <url>       Start the run via a running server instead of in-process

Inspect flags:
  --server <url>       ProvisionKit server (default http://127.0.0.1:8080)
```

`serve --workflows` accepts a single file or a directory of `.yaml` / `.yml` files. Workflow names must be unique. Without `--db`, operations live in memory and disappear when the process exits.

## HTTP API

### Start a provision

```
POST /v1/provision/{name}
Content-Type: application/json

{"inputs": {"customer_id": "acme", "plan": "standard"}}
```

`202 Accepted`:

```json
{
  "operation_id": "op_0123456789abcdef",
  "status": "pending"
}
```

The run continues in the background. Unknown workflow names return `404`. Invalid inputs return `400`.

### Get an operation

```
GET /v1/operations/{id}
```

```json
{
  "operation_id": "op_0123456789abcdef",
  "workflow": "create-vm",
  "status": "succeeded",
  "inputs": { "customer_id": "acme", "plan": "standard" },
  "steps": {
    "create_network": {
      "id": "create_network",
      "status": "succeeded",
      "attempt": 1,
      "output": { "id": "net-1" },
      "http_status": 201
    }
  },
  "created_at": "...",
  "updated_at": "..."
}
```

Operation and step status values: `pending`, `running`, `succeeded`, `failed`. If the operation fails, `error` describes why.

## How it runs

1. Load and validate YAML (structure, unique IDs, DAG, interpolations).
2. Create an operation in the store with every step `pending`.
3. Repeatedly pick steps whose dependencies have succeeded and run them, up to the concurrency limit.
4. For each attempt, interpolate the request, send it, and record HTTP status plus JSON output.
5. On success, dependents become runnable. On exhaustion of retries, the operation fails and remaining steps stay pending.

Packages:

| Package | Role |
| --- | --- |
| `cmd/provisionkit` | CLI |
| `internal/workflow` | Parse, validate, DAG |
| `internal/engine` | Execution, interpolation, retries |
| `internal/api` | HTTP server |
| `internal/store` | In-memory or SQLite persistence |
