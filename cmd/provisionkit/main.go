package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/matteogaldi/provision-kit/internal/api"
	"github.com/matteogaldi/provision-kit/internal/engine"
	"github.com/matteogaldi/provision-kit/internal/store"
	"github.com/matteogaldi/provision-kit/internal/workflow"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "graph":
		err = cmdGraph(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "inspect":
		err = cmdInspect(os.Args[2:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `Usage: provisionkit <command> [arguments]

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
`)
}

func cmdValidate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: provisionkit validate <file>")
	}
	if _, err := workflow.Load(args[0]); err != nil {
		return err
	}
	fmt.Println("valid")
	return nil
}

func cmdGraph(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: provisionkit graph <file>")
	}
	def, err := workflow.Load(args[0])
	if err != nil {
		return err
	}
	for _, id := range def.Graph.Order {
		step, _ := def.Step(id)
		fmt.Println(id)
		if len(step.DependsOn) > 0 {
			fmt.Printf("  depends_on: %s\n", strings.Join(step.DependsOn, ", "))
		}
	}
	return nil
}

func cmdServe(args []string) error {
	fs := newFlagSet("serve")
	workflows := fs.String("workflows", "", "workflow file or directory")
	addr := fs.String("addr", ":8080", "listen address")
	dbPath := fs.String("db", "", "sqlite database path")
	concurrency := fs.Int("concurrency", 8, "max parallel ready steps")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workflows == "" {
		return fmt.Errorf("usage: provisionkit serve --workflows <path>")
	}

	defs, err := workflow.LoadAll(*workflows)
	if err != nil {
		return err
	}

	var st store.Store
	if *dbPath == "" {
		st = store.NewMemory()
	} else {
		sqlStore, err := store.OpenSQLite(*dbPath)
		if err != nil {
			return err
		}
		defer sqlStore.Close()
		st = sqlStore
	}

	eng := engine.New(engine.Options{Store: st, MaxParallel: *concurrency})
	srv := api.New(defs, st, eng)
	fmt.Fprintf(os.Stderr, "provisionkit listening on %s (%d workflow(s))\n", *addr, len(defs))
	return http.ListenAndServe(*addr, srv.Handler())
}

func cmdRun(args []string) error {
	fs := newFlagSet("run")
	server := fs.String("server", "", "remote ProvisionKit server")
	inputs := &inputFlags{}
	fs.Var(inputs, "input", "key=value")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: provisionkit run <file-or-name> [--input key=value] [--server url]")
	}
	target := fs.Arg(0)

	if *server != "" {
		name := target
		if _, err := os.Stat(target); err == nil {
			def, err := workflow.Load(target)
			if err != nil {
				return err
			}
			name = def.Name
		}
		return runRemote(*server, name, inputs.Map())
	}

	def, err := workflow.Load(target)
	if err != nil {
		return err
	}
	st := store.NewMemory()
	eng := engine.New(engine.Options{Store: st})
	op, runErr := eng.Run(context.Background(), def, inputs.Map())
	if op != nil {
		if err := writeJSON(os.Stdout, op); err != nil {
			return err
		}
	}
	return runErr
}

func cmdInspect(args []string) error {
	fs := newFlagSet("inspect")
	server := fs.String("server", "http://127.0.0.1:8080", "remote ProvisionKit server")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: provisionkit inspect <operation-id> [--server url]")
	}
	op, err := fetchOperation(*server, fs.Arg(0))
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, op)
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

type inputFlags struct {
	m map[string]string
}

func (f *inputFlags) String() string { return "" }

func (f *inputFlags) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("invalid --input %q (want key=value)", v)
	}
	if f.m == nil {
		f.m = map[string]string{}
	}
	f.m[k] = val
	return nil
}

func (f *inputFlags) Map() map[string]string {
	if f == nil || f.m == nil {
		return map[string]string{}
	}
	return f.m
}

func runRemote(server, name string, inputs map[string]string) error {
	payload, err := json.Marshal(map[string]any{"inputs": inputs})
	if err != nil {
		return err
	}
	url := strings.TrimRight(server, "/") + "/v1/provision/" + name
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("server %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var start struct {
		OperationID string `json:"operation_id"`
	}
	if err := json.Unmarshal(body, &start); err != nil {
		return err
	}
	if start.OperationID == "" {
		return fmt.Errorf("server did not return operation_id")
	}

	deadline := time.Now().Add(5 * time.Minute)
	for {
		op, err := fetchOperation(server, start.OperationID)
		if err != nil {
			return err
		}
		switch op.Status {
		case store.StatusSucceeded, store.StatusFailed:
			if err := writeJSON(os.Stdout, op); err != nil {
				return err
			}
			if op.Status == store.StatusFailed {
				if op.Error != "" {
					return fmt.Errorf("operation failed: %s", op.Error)
				}
				return fmt.Errorf("operation failed")
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", start.OperationID)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func fetchOperation(server, id string) (*store.Operation, error) {
	url := strings.TrimRight(server, "/") + "/v1/operations/" + id
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var op store.Operation
	if err := json.Unmarshal(body, &op); err != nil {
		return nil, err
	}
	return &op, nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
