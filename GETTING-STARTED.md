# Getting started

This guide is written for a coding agent (or a person) driving the repository from
a clean checkout. Follow the steps in order. Each step says what a good result
looks like, so you can tell whether to continue or stop and investigate.

## Prerequisites

- Go 1.26.3 or newer on `PATH`. Check with `go version`.
- No network access is needed after the initial clone. The library has zero
  external module dependencies and every test uses in-memory fakes.

## 1. Get the code

```sh
git clone <this-repo-url> model-router
cd model-router
```

Good result: a directory containing `go.mod`, a `cmd/` folder, and the package
folders `router`, `modeltypes`, `tool`, `cost`, `laddercfg`, `jobprofile`, and
`escalation`.

## 2. Build

```sh
go build ./...
```

Good result: the command exits 0 and prints nothing. A build error here means the
toolchain is older than the version in `go.mod`, so check `go version` first.

## 3. Vet

```sh
go vet ./...
```

Good result: exits 0 with no output.

## 4. Test

```sh
go test ./...
```

Good result: every package line reads `ok`, except `jobprofile`, which has no tests
of its own and reads `[no test files]`. There is no network call and no timing
dependency, so a green run should be immediate and repeatable.

If you want to see each test name as it runs:

```sh
go test ./... -v
```

## 5. Run the demo

```sh
go run ./cmd/routerdemo
```

Good result: three labelled scenarios print to stdout.

1. **A cheap request stays on the local tier.** The router serves the free
   `qwen3.8-27b` local model and a clean turn produces no transition.
2. **A model-call fault escalates one tier.** A context-overflow fault moves the
   router from the local model up to the mid model, and the next turn runs there.
3. **The frontier rung is usage-capped.** After two tool-error turns the router
   reaches the frontier model. It serves one bounded turn, and the next climb is
   refused and bounced down to the mid tier. The final line reports one frontier
   turn served against a cap of one, and one climb blocked.

Each served line also shows the model's cost tier (`local`, `mid`, or `strong`) and
the per-turn tool-round budget the ladder policy grants that rung, which is larger
for the cheap rung and smaller for the expensive one.

## Where to read next

- `router/router.go` is the core. Start at the package comment, then read
  `NextAdapter`, `Observe`, and the `EscalateFor*` methods.
- `router/*_test.go` are table-driven and double as worked examples of every
  routing and escalation path.
- `cmd/routerdemo/main.go` is a short, legible driver you can copy from to wire the
  router into your own loop.
