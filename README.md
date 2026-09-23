# model-router

A cost-aware router that picks which model tier should handle each turn of an
agent loop. It starts every worker on the cheapest model that might do the job and
climbs to a stronger, more expensive model only when the turn gives it a concrete
reason to. When the trouble passes, it walks back down toward the cheap tier.

The whole thing is pure Go with no external dependencies. The `require` block in
`go.mod` is empty on purpose.

## Why it exists

Running every turn on a frontier model is simple and expensive. Running everything
on a cheap local model is cheap and often not good enough. The useful middle is to
default to the cheap tier and escalate on evidence, so the expensive tier only gets
used when a cheaper one has actually failed.

This router is the piece that makes that decision. It holds an ordered ladder of
model tiers and a small state machine that moves a worker up and down the ladder
based on per-turn signals and mid-turn faults.

## The design

**An ordered tier ladder.** You give the router a slice of adapters sorted from
cheapest to strongest (for example: a free local model, a cheap hosted model, a
frontier model). Rung 0 is the floor a worker rests on. A two-tier `{cheap,
strong}` setup is just a two-rung ladder.

**A two-call contract per turn.** At the top of a turn the caller asks
`NextAdapter()` for the model to run. After the turn the caller feeds the result
back with `Observe(signals)`, which returns an `Edge` describing any tier change.
`NextAdapter` also handles availability: if the chosen rung cannot serve right now,
a cold low rung climbs to the first available higher rung, and an unavailable
higher rung degrades back toward the floor, so a turn is never handed to an adapter
that would fail on the call.

**A closed set of escalation triggers.** `Observe` climbs one rung when the
highest-priority enabled trigger fires: repeated tool errors, parse failures, retry
exhaustion, low self-reported confidence, or an explicit handoff request. Each
trigger has a threshold, and the set is fixed rather than open-ended, so the
routing decision stays explainable. After a configurable number of consecutive
clean turns the router descends one rung.

**Event-driven escalation for faults.** Some failures happen mid-turn and should
lift the rung immediately rather than waiting for the turn boundary. The router
exposes explicit methods for these: a context overflow the loop cannot compact
away, a repeated timeout, a malformed tool call, a stuck verify gate, a stalled
no-progress loop, and a spent round budget. A rate-limit on a paid rung does the
opposite and drops straight back to the free floor, since retrying a throttled rung
is pointless.

**A bounded frontier.** The strongest rung can be capped with `WithBoundedTop(n)`
so it serves at most `n` turns per session. Once that budget is spent, a further
climb onto it is refused and the router stays one rung below. A shared
`StrongBudget` extends the same cap across a whole tree of spawned workers, so
respawns cannot each re-climb the frontier and blow the run's cost.

**Pure and sans-IO behind the `Adapter` seam.** The router never makes a network
call. It talks to models through the `modeltypes.Adapter` interface, which has
`Model()`, `Available()`, and `Complete(...)`. Tests use an in-memory `Echo`
adapter, so the entire routing engine is exercised with no backend and no network.
Emitting telemetry or escalation events is the caller's job, not the router's.

## Packages

| Package       | What it holds                                                             |
|---------------|---------------------------------------------------------------------------|
| `router`      | The ladder and the escalation state machine.                              |
| `modeltypes`  | The `Adapter` interface, the message/response value types, the fault taxonomy, and the `Echo` test fake. |
| `adapters`    | Real `Adapter` implementations — `OpenAICompat` (a local llama.cpp server or any OpenAI-compatible endpoint) and `Anthropic` — plus the `FaultOf` helper that recovers a fault kind from a failed call. |
| `tool`        | The provider-agnostic tool-call and tool-result types plus an error tally. |
| `cost`        | A price table and per-tier classifier, a running ledger, and shared spend/turn budgets. |
| `laddercfg`   | Pure ladder-sizing policy: context-window budgets and rung-proportional round budgets. |
| `jobprofile`  | The tier and capability envelope a worker runs under.                     |
| `escalation`  | A thin client that emits an escalation decision through an injected dispatcher. |

## Build, test, run

```sh
go build ./...
go vet ./...
go test ./...
go run ./cmd/routerdemo
```

The tests are sans-IO and hold a 95% statement-coverage floor over the logic
packages (currently 98.7%). The two `cmd/` demo mains are thin drivers and are
excluded from the floor.

The demo builds a local/mid/strong ladder of fake adapters and prints three
scenarios: a cheap request that stays on the local tier, a fault that escalates one
tier, and a frontier rung that refuses a climb once its per-session cap is spent.

## Real models

The demo above uses fakes. To route real models, the `adapters` package ships two
`modeltypes.Adapter` implementations: `OpenAICompat` for a local llama.cpp server
or any OpenAI-compatible endpoint, and `Anthropic` for the Messages API. You build
one from a struct literal and hand it to the router in place of a fake.

`cmd/routerdemo-live` does exactly that, building a ladder from environment
variables:

```sh
# a local llama.cpp server as the cheap tier
export LLAMA_BASE_URL=http://localhost:8081/v1
export LLAMA_MODEL=your-served-model

# Anthropic as the fallback tier
export ANTHROPIC_API_KEY=sk-ant-...
export ANTHROPIC_MODEL=claude-haiku-4-5-20251001

go run ./cmd/routerdemo-live
```

Each rung is optional. The command runs a real turn on the cheapest rung; with a
second rung set it simulates a context-overflow fault and shows the real fallback.
With neither variable set it prints what to set and exits non-zero.

Wiring your own loop is the same shape: construct the adapters cheapest to
strongest, pass them to `router.NewLadder`, and drive the turns. When a call
fails, recover its fault kind with `adapters.FaultOf(err)` and pass it to
`router.EscalateForFault`, which is how a real loop climbs the ladder on a fault.

## Provenance

This is a self-contained extract from a larger private agent operating system. The
router, the ladder-sizing policy, the tool and cost types, and the model value
types were lifted out as a coherent subset and given clean package paths so the
routing engine can stand on its own with zero dependencies. The escalation policy
and the cost model are the real ones the larger system uses; the model ids and
prices in the cost table are examples you would replace with your own.

The `adapters` package and `cmd/routerdemo-live` are written for this repository,
not lifted from the private system, which keeps its real adapters internal. They
are hand-owned here: a future sync of the extracted core leaves them untouched.
