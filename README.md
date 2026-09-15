# ProvisionKit

ProvisionKit is an experimental Go runner for YAML-defined HTTP provisioning workflows. Steps form a directed acyclic graph (DAG): each step calls another service, and later steps can use earlier JSON responses. Use it to orchestrate multi-service provisioning — create an account, then a network, then a VM — without writing a custom runner for each flow.

You can validate and execute a workflow from the CLI, or start an HTTP server that accepts provision requests and tracks each run as an operation.

## Status and limitations

This is an early prototype for experimentation in trusted environments. It supports dependency validation, parallel execution, retries, and operation tracking. It is not yet ready for production provisioning.

- **Retries can duplicate resources.** ProvisionKit does not automatically deduplicate requests or provide idempotency keys. A timed-out request may have succeeded remotely before it is retried. Configure retries only when repeating the remote action is safe; all attempt errors are currently retried, including permanent HTTP errors.
- **Failures do not roll back completed actions.** If a later step fails, resources created by earlier steps remain. Inspect the operation and remote services before cleaning up or starting a new run.
- **SQLite preserves records, but does not recover executions.** Unfinished operations are not resumed or marked interrupted on restart and may remain `pending` or `running`. There is no automatic recovery or resume command.
- **HTTP success does not guarantee resource readiness.** Every 2xx response, including `202 Accepted`, completes a step. ProvisionKit does not poll remote jobs for completion before starting dependent steps.
- **The HTTP API has no authentication or authorization.** Anyone who can reach it can start workflows and retrieve operations by ID. Bind to loopback for local use; deployment beyond that requires access controls.
- **Operation data is not redacted.** Inputs, JSON outputs, and error details can be stored and returned through the API. Avoid passing secrets as workflow inputs or using sensitive response data in demos.

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

The included HTTPBin demo sends sample JSON to the public `https://httpbin.org` service and passes its echoed response to a second request. It requires internet access and HTTPBin availability. Use sample values only.

Validate the workflow, print its execution order, then run it in-process:

```bash
./provisionkit validate examples/post-httpbin.yaml
./provisionkit graph examples/post-httpbin.yaml
./provisionkit run \
  --input customer_id=acme \
  --input plan=standard \
  examples/post-httpbin.yaml
```

`run` executes in-process, prints the operation as JSON, and exits non-zero if any step fails.

Place flags before the workflow path or name; the CLI stops parsing flags at the first positional argument.

To expose the demo over a local HTTP API:

```bash
./provisionkit serve --workflows examples/post-httpbin.yaml --addr 127.0.0.1:8080
```

Then start a run by workflow name:

```bash
curl -sS -X POST http://127.0.0.1:8080/v1/provision/post-httpbin \
  -H 'Content-Type: application/json' \
  -d '{"inputs":{"customer_id":"acme","plan":"standard"}}'
```

The server responds with `202 Accepted` and an `operation_id`. Substitute that ID below to check status:

```bash
./provisionkit inspect op_0123456789abcdef
# or
curl -sS http://127.0.0.1:8080/v1/operations/op_0123456789abcdef
```

`run --server` does the same POST + poll loop for you:

```bash
./provisionkit run --server http://127.0.0.1:8080 \
  --input customer_id=acme \
  --input plan=standard \
  post-httpbin
```

## Workflows

A workflow is a YAML file. `config.yaml` and `examples/create-vm.yaml` illustrate provisioning flows with placeholder `.test` service URLs. Replace those URLs and adapt the requests to your services before running them. `examples/post-httpbin.yaml` is the runnable public-service demo used above.

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
  run [flags] <file-or-name>   Run a workflow
  inspect [flags] <operation-id> Inspect an operation

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

`serve --workflows` accepts a single file or a directory of `.yaml` / `.yml` files. Workflow names must be unique. Without `--db`, operations live in memory and disappear when the process exits. With `--db`, records persist, but unfinished executions are not recovered on restart.

The default `--addr :8080` listens on all interfaces. Use `--addr 127.0.0.1:8080` for local experimentation.

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
