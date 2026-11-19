# Drone target detection

**Design a system to help operators understand why drones are missing target detection events.**

This folder has two parts:

1. **The system design.** Interview-style notes on the problem, the requirements and the target
   architecture. They follow the interview stages in
   [design-interviews](https://github.com/shervinrad100/design-interviews#readme).
2. **A working prototype in Go.** Mock services recreate the pipeline (drone camera → ground
   receiver → inference → …), so the logging, metrics and tracing design can be built and tested for
   real. It's also a hands-on way to learn Go, Kafka and OpenTelemetry.

| File | What's in it |
|---|---|
| `README.md` (this file) | The problem, the design, the prototype's architecture and how to run it |
| [`docker-compose.yml`](docker-compose.yml) | Runs the prototype |
| `src/` | One Go module: `services/` (one program each, with its own README) and `internal/` (shared packages) |

---

## 1. The problem

### Scenario
An operator flies a drone and scans a field for a target. The footage is sent back for analysis. A
decision is made and sent back to the drone, but it isn't acted on: the detection event was
delivered and then missed. We want to know **why**.

### Scope
- **Gather the data first.** We don't know why detections are missed, so we can't fix it yet.
  Ideas for fixing it are a deep dive for later.
- There are two goals, so there are two systems:
  - **Live decisioning:** spot the problem as it happens.
  - **Model improvement:** use the collected data offline to improve the models.

### Clarifying questions
- Are we focusing on logging, or on solving target detection? What's the scope once we find the problem?
  - Gather the data first. We can't fix what we don't understand.
- Is the goal to fix our models, or to solve the problem in the moment?
  - Design two systems, one for each.
- Do we have storage constraints? Can we save all the videos?
  - No constraints on the receiver side. 
  - Drone bandwidth is limited. 
  - Drone security - what if it's shot down and recovered?
- Could someone be jamming the signal?
  - yes
- Is the operator platform separate from central command, where the ML models run?
  - Assume not.

---

## 2. Live decisioning design

### Where a detection can be missed
- **Video:** is the person blocking their face? Is the lighting poor, or the weather bad? Is the
  target too far away, or is something blocking the view?
- **Network:** is the drone's connection the problem, either making the video poor or delaying the
  decision?
- **Model:** does the decision take so long that we miss the action window?
- **Workflow:** does acting on it take too long? Or is the process automated, and the drone's own
  model overrides the event it receives?

Also open: what are the acceptable false positive and false negative rates?

### plan

1. Video capture
2. Video receiver
3. Inference model
4. Post-processing and event delivery
5. Action workflow

For each one: what do we log, and how?

### Functional requirements
- Build a timeline of events by aggregating logs from all the sources (mock the sources).
- Realtime.
- No storage limits within reason, with a retention policy.
- Limited network bandwidth.
- Limited hardware for processing on the drone.
- metric ingestion
- querying and visualisation 
- alerting
- notifications.

### Non-functional requirements
- Disruptions: what if cloud cover slows the link down?
- What if the signal is jammed?
- Visuals that users can interact with.
- If the drone is shot down, its data must not be recoverable.
- Can others tap into the feed? If so, security and encryption.
- Fault tolerance.
- Scale to large amounts of data from various sources.
- high write availability 
- low latency
- query performance
- storage efficiency.

### Core entities
- Camera feed (the drone)
- Receiver (ground station)
- ML inference model
- ML decision model
- Operator platform

### What to build

- Mock a video producer that emits 30–60 FPS and logs, with random bytes to mimic a real feed.
- Send it over the network.
- At the receiver, measure connection quality (packet loss, ordering, reconnections) and speed, and
  video quality over the network.
- A consumer for the frames that simulates the inference model: a dummy function that waits x
  seconds, with logs.
- A decision aggregator that builds action events: a dummy function that waits x seconds, with logs.
- Aggregate all log streams into one timeline and present it.
- Services find each other through a **service registry** (built by hand, as a learning exercise).

### architecture

![architecture](Screenshot%20from%202026-09-18%2020-14-52.png)

- Everything except the drone runs in one VPC. The drone streams video to the receiver over TCP.
  - The drone has **no logging infrastructure**. The frame ID and send time travel in each frame's
    header, and the receiver does all the measuring.
- The receiver buffers the stream and sends frames to video storage and the inference model. The
  inference model feeds the decision model, which feeds the operator platform.
- Every stage writes logs to its own Kafka topic.
  - Apps write JSON logs to stdout. The **OpenTelemetry Collector** reads them and ships them to Kafka,
    one topic per service (`logs.<service>`). Apps never block on Kafka.
  - **Kafka carries logs only.** Frames go to inference and storage over a direct, fast path.
- A **log aggregator** consumes all the topics, produces metrics and stores a timeline in
  Elasticsearch. A **retention job** moves old data to cold storage.
  - **Metrics** (fps, drops, delay) go to Prometheus. **Traces** use the frame ID as the trace ID,
    with one span per stage.
- A **stream processor** consumes the same topics for alerting. Redis tracks which alerts were
  already sent, and a **messaging service** notifies the operator. (Alert noise is its own problem:
  see [noisy-alerts](https://github.com/shervinrad100/design-interviews/tree/main/noisy-alerts).)


### Storage and API reference

**Data model:**
- A store with high write speed. ~~Cassandra~~ Elasticsearch, because Cassandra can't query by any
  key other than the partition key (usually time).
  - Logs are time series, so they're easy to sequence, and they're immutable.
  - Fetching by key works well, and the key will usually be time-based.
- Postgres for standard metadata.
- Redis for caching.
- Kafka for buffers and queues: persistent and tolerant of spikes.


---

## 3. Prototype

The prototype is one Go module in `src/`. This section is the map: how the services connect and
where to find them. Each service has a README next to its code with how it works, how to run it and
its open work. The two contracts every service shares, the frame protocol and the logging
conventions, are documented here.

### How it fits together

- **Data path.** [video-feed](src/services/video-feed/README.md) plays the drone: it makes frames and
  sends them over TCP through a simulated bad network.
  [video-receiver](src/services/video-receiver/README.md) is the ground station: it reads every
  frame, measures the link (drops, delay, fps) and forwards the frame to
  [inference-receiver](src/services/inference-receiver/README.md) and
  [video-store](src/services/video-store/README.md). Detections then flow on to the decision model
  and the operator platform (section 2).
- **Service discovery.** [svcregistry](src/services/svcregistry/README.md) is a phone book, not a
  proxy. Services register their address and send heartbeats. The receiver looks up where to send
  frames and then connects directly, so the registry stays off the data path.
- **Observability.** Every service writes JSON logs to stdout and knows nothing about Kafka. The
  OpenTelemetry Collector ships the logs to Kafka, one topic per service (`logs.<service>`). Metrics
  go to Prometheus, and traces use the frame ID. The drone's logs are for local debugging only: a
  real drone's stdout can't be reached from the ground, so the pipeline doesn't collect them.

### Where the code is

```
src/
├── internal/                packages the services share
│   ├── platform/            NewLogger(): the JSON logger every service uses
│   ├── svc/                 registry types and client, timings, and the service-name check
│   ├── video/               the frame protocol
│   └── ...                  other packages I decide to add in the future
└── services/                one program each, with its own Dockerfile and README
    ├── video-feed/          the drone's camera
    ├── video-receiver/      the ground station
    ├── inference-receiver/  runs the ML model on each frame
    ├── video-store/         the raw video archive
    ├── svcregistry/         service discovery
    └── ...                  other services I decide to add in the future
```

### Frame protocol
Every frame is a 20-byte header followed by the payload. All integers are big-endian.

```
| id     | sent_at (Unix ns) | payload length | payload   |
| uint32 | uint64            | uint64         | ≤ 1 MiB   |
| 4 B    | 8 B               | 8 B            |           |
```

`ReadFrame` returns `io.EOF` only when the stream ends cleanly between frames, and
`io.ErrUnexpectedEOF` when it stops mid-frame. It rejects a length over 1 MiB before allocating, so a
corrupt header can't exhaust memory.

### Logging conventions
- JSON lines to stdout through `log/slog`. Nothing writes to Kafka directly.
- Attribute names follow OpenTelemetry semantic conventions: `remote.address`, `local.address`,
  `network.transport`, `network.connections.active`.
- Every line carries `service.name`, which the Collector uses to pick the `logs.<service>` topic. A
  service reads it from `OTEL_SERVICE_NAME` at startup and exits if it's missing or isn't a valid
  name: lowercase letters, digits and `-`, at most 63 characters. The registry accepts the same
  names. video-feed doesn't read one, because the drone's logs aren't collected.
- `LOG_LEVEL` sets the level: `DEBUG`, `INFO`, `WARN` or `ERROR`, in any case. If it's empty, the
  level is `INFO`. If it's invalid, the level is `INFO` and a warning is logged.

### Running it
From this folder:

```bash
docker compose up --build              # build and start every service
docker compose logs -f video-receiver  # follow the ground station's logs
docker compose down                    # stop and remove the containers
```

Compose sets each service's flags and `OTEL_SERVICE_NAME`. Each service's README lists its flags.

---

## 4. Principles

### Layers
Logging, network and env vars stay at the edges. The domain code only computes.

```
┌─ platform ──────────────────────────────────────────┐
│  logger from env, config, shutdown signals          │
│  ┌─ adapters (edges) ────────────────────────────┐  │
│  │  TCP listener, chaos writer, sinks, Kafka     │  │
│  │  ┌─ domain ────────────────────────────────┐  │  │
│  │  │  frame encode/decode, stats math        │  │  │
│  │  │  (no logging, no network, no env vars)  │  │  │
│  │  └─────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

### Logs, metrics and traces
Good observability needs three things: logs for humans, metrics, and traces. Metrics aren't printed
as log lines; they're stored as typed metrics:

| Type | Behaves like | Our stats |
|---|---|---|
| Counter | Only goes up, never resets | `frames_received_total`, `frames_dropped_total` |
| Gauge | Goes up and down | `connections_active` |
| Histogram | Counts values into buckets (≤16 ms, ≤100 ms, ≤1 s, ≤5 s…) | `frame_delay_seconds`: gives p50/p99, not just avg/max |

fps isn't stored at all. The query works it out from the counter: `rate(frames_received_total[1m])`
means "how fast did this counter grow over the last minute". Because the raw totals are stored, any
time window and any statistic can be calculated afterwards.

There are two ways to get the numbers out of the app:
- **Pull (Prometheus, the most common):** the app serves the current totals at an HTTP endpoint,
  `/metrics`. Prometheus visits it about every 15 s ("scrapes" it) and stores the values, and Grafana
  draws the charts. If the stream stalls, the counter stops growing and fps drops to 0. That's
  visible and you can alert on it, unlike a log line that's only written when a frame arrives.
- **Push (OpenTelemetry SDK):** the app sends metrics to a collector, which forwards them to
  Prometheus or another backend. It's vendor-neutral, with a bit more setup.
