# video-receiver

The ground station
It accepts the drone's stream, measures the health of the link, and will forward every frame to inference and storage.

## How it works

- Listens on TCP and serves one connection at a time (`MAX_CONNECTIONS = 1`). A second connection is
  closed straight away rather than left waiting.
- Reads one whole frame at a time with `video.ReadFrame` (see the
  [frame protocol](../../../README.md#frame-protocol)). It closes the connection when the stream ends
  or a frame can't be read.
- Measures each frame (below).
- **Forwarding isn't wired yet.** The `io.MultiWriter` to inference-receiver and video-store is
  commented out. Their addresses will come from [svcregistry](../svcregistry/README.md).

## What it measures

`streamStats` logs a `Stream stats` line for each 1-second window, plus one for the last partial
window when the connection closes:

| Field | Meaning |
|---|---|
| `frames.received` | Frames that arrived |
| `frames.dropped` | Gaps in frame IDs: frames lost anywhere upstream, on the drone or on the link |
| `frames.delayed` | Frames older than one frame interval (1/60 s) when they arrived |
| `delay_ms.avg`, `delay_ms.max` | Time from the frame's `sent_at` to its arrival |
| `fps.avg` | Frames received ÷ the window's length |

Limits:
- The window is only checked when a frame arrives, so a stalled stream logs nothing.
- Delay is only accurate when the drone's and the receiver's clocks agree, as they do on one host.

## Running it

| Flag | What it sets | Default | In compose |
|---|---|---|---|
| `-port` | The port to listen on | none: without it the OS picks a random free port | `8080` |

`LOG_LEVEL` sets the log level (compose: `DEBUG`). `OTEL_SERVICE_NAME` sets `service.name` on its log
lines (compose: `video-receiver`). It's required: without a valid name the service exits at startup.

## Code

All of it is in `video-receiver.go`: `main` runs the accept loop, `handleVideoFeed` handles one
connection, and `streamStats` does the measuring.

## Open work

- [ ] Forward frames to inference-receiver and video-store, looking up their addresses in the
  registry. On an error, look up again and retry another instance.
- [ ] Forward the whole encoded frame (`video.EncodeFrame(frame)`), not `frame.Payload`. Downstream
  services read with `video.ReadFrame`, which expects the header, and the frame ID is needed later as
  the trace ID.
- [ ] Decide the fan-out: the receiver pushes to storage and inference, or they pull from it.
  `io.MultiWriter` writes one destination after another and stops at the first error, so a slow or
  dead storage would stall inference.
- [ ] Replace the per-second `Stream stats` line with one log line per frame
  (`event.name=frame.received`, `frame.id`, `frame.sent_at`, `frame.delay_ms`, `frame.gap`,
  `frame.payload_bytes`), and move fps, drops and delay to Prometheus metrics. Logs hold raw facts;
  aggregating them is the metrics system's job. This also separates measuring from reporting, which
  `streamStats.report` combines today.
- [ ] `main()` and `handleVideoFeed` do everything: config, listen, accept loop, read, stats and
  forward. Split them so each piece can be tested or swapped, e.g. a `Server` struct that owns the
  connection loop.
- [ ] Graceful shutdown: stop the accept loop with a `context.Context` from `signal.NotifyContext`.
- [ ] Loggers are used two ways: `streamStats.report` takes one as an argument, and
  `handleVideoFeed` builds one from the global `slog`. Pick one.
- [ ] Constants are `SCREAMING_CASE`, and Go uses `MixedCaps`.
- [ ] Tests for `internal/video`, which the receiver reads frames with: a round trip, an oversized
  length and a truncated payload. Its constants are `SCREAMING_CASE` too.
- [ ] Remove the stale `// TODO add logging` at the top of `video-receiver.go`.
