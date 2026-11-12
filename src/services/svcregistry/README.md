# svcregistry

Service discovery, built by hand as a learning exercise. It's a phone book, not a proxy: services
register their address and send heartbeats, and a client looks up an address and then connects
directly, so the registry stays off the data path.

## What it does today

| Part | Where | State |
|---|---|---|
| Registry logic | `registry.go` | Done and tested |
| HTTP server | `main.go` | Routes registered; every handler returns 500 "not implemented yet" |
| Client (`RegisterService`, `Heartbeat`) | [`internal/svc`](../../internal/svc/service.go) | Empty stubs |
| Deployment | `Dockerfile` | `# TODO`; not in `docker-compose.yml` |

## How the registry works

- `register` checks the address is `host:port` and gives the instance a new UUID. If an instance of
  that service is already at the same address, it's replaced: an address only registers again after
  a restart, so the old entry is stale.
- `lookup` and `list` return snapshots: copies the caller can't use to change the registry.
- `renewLease` is the heartbeat. It updates `lastSeen`, and rejects an instance marked `down`, which
  must register again.
- `deregister` marks an instance `down`, so a service can shut down gracefully.
- `monitor` runs `cleanup` every second. Cleanup removes instances past the TTL or marked `down`,
  then any service with no instances left.
- One `sync.RWMutex` guards the map: lookups share it, and writes and cleanup take it alone.

Timings, from `internal/svc`: a heartbeat every 3 s, and a TTL of 3 heartbeats + 1 s = 10 s. With
cleanup every second, an instance that stops heartbeating is removed 10–11 s after its last
heartbeat. A `down` instance still shows up in `lookup` until the next cleanup.

## HTTP API

The server calls `http.ListenAndServe("")`, which listens on port 80.

| Method | Path | Will call |
|---|---|---|
| `POST` | `/register` | `register` |
| `GET` | `/service/` | `list` |
| `GET` | `/service/{serviceId}` | `lookup` |
| `PUT` | `/service/{serviceId}/delete/{instanceId}` | `deregister` |
| `GET` | `/service/{serviceId}/ping/{instanceId}` | `renewLease` (the heartbeat) |

## Tests

From `src/`:

```bash
go test -race ./services/svcregistry/
go test -tags perf -run 'Memory|Stall' -bench . -benchmem -v ./services/svcregistry
```

- `registry_test.go`: every registry method, the TTL boundary, the monitor loop, and concurrent
  access (run it with `-race`).
- `main_test.go`: checks the shape of the register response. It fails until the handler is written.
- `registry_performance_test.go` (build tag `performance`, about 20 s): benchmarks for `cleanup`, `renewLease` and
  `register`, memory per instance, and a stall test. The stall test holds the write lock for 6, 9
  and 12 s, standing in for a log write stuck on a full stdout pipe, and counts the live instances
  the registry drops. The file's header explains how to run it on a Raspberry Pi.

## Open work

- [ ] Implement the HTTP handlers. The routes are registered, but every handler returns 500.
- [ ] A Dockerfile (it's `# TODO` now) and a compose service.
- [ ] Client in `internal/svc`: `RegisterService` and `Heartbeat`. If the registry answers 404 (the
  reaper removed the instance), register again. If it's unreachable or returns a 5xx, keep the same ID
  and retry with backoff. The client caches the last registry it saw. Once registered, a service
  adds its instance ID to its log lines.
- [ ] Where does a service's advertised address come from: Docker, or an env var?
- [ ] `lookup` returns `down` instances until the next cleanup pass. Filter them out on read, or let
  clients skip them.
- [ ] `register`: accept a description. What if the name matches but the description doesn't?
- [ ] `renewLease`: does it need to return the instance, or just an error?
- [ ] Graceful shutdown: `main` waits on `ctx.Done()`, but nothing cancels it, and
  `http.ListenAndServe` blocks before it's reached. Use `signal.NotifyContext`.
- [ ] Pass the service name to `platform.NewLogger`, so every log line says which service wrote it
  (`service.name`). The collector routes logs to `logs.<service>` by it.
- [ ] Constants are `SCREAMING_CASE` (`REGISTRY_UPDATE_LOOP` here, `HEARTBEAT_INTERVAL` and `TTL` in
  `internal/svc`), and Go uses `MixedCaps`.
- [ ] Later: cleanup is O(services × instances) per tick, so optimise it; gRPC; push deregistrations
  to the services that depend on the one that left (service lineage); a CLI that creates and
  registers test services.

How graceful shutdown should work:
1. Move the body of main into func run() error. Then main only needs if err := run(); err != nil { slog.Error(...); os.Exit(1) }. os.Exit skips deferred calls, so keep all your defers inside run.
2. Use signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM). docker stop sends SIGTERM. Your context.Cause(ctx) log in monitor will then name the signal for free (I checked the docs).
3. Run srv.ListenAndServe() in a goroutine and send its error on a channel with a buffer of 1.
4. select on ctx.Done() and on that error channel.
5. Call srv.Shutdown(...) with a new context.WithTimeout(context.Background(), 5*time.Second).
6. wg.Wait() for the monitor.