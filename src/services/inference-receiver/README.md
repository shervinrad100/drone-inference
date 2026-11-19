# inference-receiver

Runs the ML model on each frame and passes detections to the decision model.

## What it does today

- Listens on TCP and serves one connection at a time (`MAX_CONNECTIONS = 1`).
- Reads frames with `video.ReadFrame` and writes each payload to `MachineLearningModel`, whose `Write`
  does nothing. No inference latency is simulated and no detection is produced.
- Nothing sends it frames yet: [video-receiver](../video-receiver/README.md) doesn't forward them.

## Running it

| Flag | What it sets | Default | In compose |
|---|---|---|---|
| `-port` | The port to listen on | `8080` | `8082` |

`LOG_LEVEL` sets the log level (compose: `WARN`). `OTEL_SERVICE_NAME` sets `service.name` on its log
lines (compose: `inference-receiver`). It's required: without a valid name the service exits at
startup.

## Code

All of it is in `inference-receiver.go`: `main` runs the accept loop, `handleVideoFeed` reads one
connection, and `MachineLearningModel` stands in for the model.

## Open work

- [ ] **Bug: it spins on read errors.** `handleVideoFeed` returns only on `io.EOF`. Any other error,
  such as `io.ErrUnexpectedEOF` when the sender dies mid-frame, a connection reset, or an oversized
  length, is logged and the loop carries on. It retries the broken connection forever at full CPU,
  writes empty frames to the model, and never frees its connection slot. Commit `0d5f25e` fixed
  this, and `0163ea1` moved the `return` into the `io.EOF` branch.
- [ ] Simulate inference: `Write` does nothing. Add a configurable delay (the "wait x seconds" in
  [what to build](../../../README.md#what-to-build)) and log the start and end of inference with the
  frame ID.
- [ ] Produce a detection result for the decision model.
- [ ] `-port` defaults to `8080`, but video-receiver has no default. Make the two consistent.
- [ ] Graceful shutdown: stop the accept loop with a `context.Context` from `signal.NotifyContext`.
- [ ] Constants are `SCREAMING_CASE`, and Go uses `MixedCaps`.
