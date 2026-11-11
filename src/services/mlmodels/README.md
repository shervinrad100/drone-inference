# inference-model

Folder `mlmodels`, compose service `inference-model`. 
Runs the ML model on each frame and passes detections to the decision model.

## What it does today

- Listens on TCP and serves one connection at a time (`MAX_CONNECTIONS = 1`).
- Reads frames with `video.ReadFrame` and writes each payload to `MachineLearningModel`, whose `Write`
  does nothing. No inference latency is simulated and no detection is produced.
- Nothing sends it frames yet: the [receiver](../receiver/README.md) doesn't forward them.

## Running it

| Flag | What it sets | Default | In compose |
|---|---|---|---|
| `-port` | The port to listen on | `8080` | `8082` |

`LOG_LEVEL` sets the log level (compose: `WARN`).

## Code

All of it is in `inference-receiver.go`: `main` runs the accept loop, `handleVideoFeed` reads one
connection, and `MachineLearningModel` stands in for the model.

## Open work

- [ ] **Bug: it spins on read errors.** `handleVideoFeed` returns only on `io.EOF`. Any other error,
  such as `io.ErrUnexpectedEOF` when the sender dies mid-frame, a connection reset, or an oversized
  length, is logged and the loop carries on. It retries the broken connection forever at full CPU,
  writes empty frames to the model, and never frees its connection slot. Commit `b717fb6` fixed
  this, and `27309f3` moved the `return` into the `io.EOF` branch.
- [ ] Simulate inference: `Write` does nothing. Add a configurable delay (the "wait x seconds" in
  [what to build](../../../README.md#what-to-build)) and log the start and end of inference with the
  frame ID.
- [ ] Produce a detection result for the decision model.
- [ ] `-port` defaults to `8080`, but the receiver has no default. Make the two consistent.
- [ ] Graceful shutdown: stop the accept loop with a `context.Context` from `signal.NotifyContext`.
- [ ] Pass the service name to `platform.NewLogger`, so every log line says which service wrote it
  (`service.name`). The collector routes logs to `logs.<service>` by it.
- [ ] Constants are `SCREAMING_CASE`, and Go uses `MixedCaps`.
