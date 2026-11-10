# Drone target detection

**Design a system to help operators understand why drones are missing target detection events.**

## clarifying questions
- Are we focusing on logging in this case or are we trying to solve the issue of target detection? What is the scope of the solution once we find out the problem?
    - lets focus on gathering the data first. We don't know why we are missing it so we can't solve it if we don't know
    - as a deep dive we can ideate on how we can fix this
- what is the goal of this are we trying to fix our models or are we trying to solve the problem in that instant?
    - design two different systems one for each
- what is the env that we are working in? Do we have stable connection so that we can stream data?
    - lets lay down this scenario: We have an operator flying a drone, they scan the field for a target. The footage is sent back for analysis. A decision is made and communicated to the drone but it's not being actioned (event delivered and missed)? Let's say yes and we want to know why
- do we have access to metadata or is it just the video footage? I'm trying to understand where we are building the system. On the drone or in the command center where the decision is being made.
    - eg meteorology and location (indoor vs outdoor) distance to target
    - also do we get information on target eg height and weight or just facial recognition
- how is the target detection done at the moment? is it based on face or is it based on time and location and other features (clothes height weight)
    - lets say multiple factors
- do we have storage constraints? Can we save all the videos? lets say no constraints
- are we working with just one model or a range of various models. (face matching and other models for other things)
- is it possible that someone is jamming signals? (think about how we can detect that)
- is the operator platform separate from central command where the ML models run? lets assume no


## live decisioning 
### further questions
- What is the acceptance rate for false positives or false negatives? explain your thought process for both scenarios where either one is prioritised. 
- where is the issue coming from? create logging across various factors:
    - video: 
        - person blocking their face? 
        - lighting insufficient? bad weather?
        - too far or other factors blocking the view?
    - network:
        - is the issue from the drone connectivity?
            - causing the video to be poor
            - or causing delay in the decisioing 
    - model:
        - does the decision take too long and we miss the action window?
    - workflow:
        - time to action is too long and misses the window?
        - is the process automated where the target event is sent to the drone where the drone also has its own model and has to take a decision which overrides the event?
 

let's break that down into chunks that we can tackle in one hour sprints. From the start
1. Video capture
2. network connection
3. inference model
4. post processing and event delivery
5. action workflow

now I can dig deep into one of these and discuss how we would be logging these. 

## (my thoughts)
### functional requirements
- build a timeline of events by aggregating logs from the various sources
    - mock the various sources
- realtime
- no storage limitations within reasonable means
    - retention policy
- network limitations with bandwidth
- hardware limitations for processing on client


### non-functional requirements
- what happens if there are disruptions? Cloud coverage slowing internet down?
- what if they jam the signal?
- visuals for users to be able to interact
- what if they shoot down the drone? we dont want the data to be recoverable on the client
- do we want others to be able to tap into the feed? 
    - security and encryption in such case?
- fault tolerance
- how do we scale this to manage large amounts of data

### core entities
- camera feed
- receiver
- ML inference model
- ML decision model
- opeartor platform

### interfaces
- mock a video producer to emit 30-60 FPS and logs
    - add a random noise to it to replicate real life feed
- transmit that over the network
- log metrics at the receiver: connectivity quality (packet loss, order, reconnections etc) and speed, video quality over the network
- you want to then build a consumer for these frames as a dummy func with wait(x seconds) to simulate inference model. with logs
- have a decision aggregator to build action events as a dummy func with wait (x sec) with logs
- aggregate all log streams into one timeline and present

## Example from yt
### functional requirements 
- metric ingestion
- querying and visualisation
- alerting
- notifications

### non functional requirements
- massive scalability
- high write availability
- low latency
- query performance
- storage efficiency

### Data model
- something with high write speeds - ~~Cassandra~~ I would use elasticsearch because if we want to query by any key other than the time we won't be able to on C*
    - logs are timeseries so easy sequencing and they're immutable
    - Good fetch by key (which will usually be time based?)
- RDB for standard metadata - Postgres
- Caching - Redis
- buffers and queues - kafka
    - presistence and spike tolerant 

### API design
- receive query and return with log data
    - POST v1/logs/query
- change the alerting settings
    - POST v1/alerts



---
coding notes:
- I'm running the ML model on the same machine as the receiver (tightly coupled import).

# TODO
- Duplicated platform code. Both main()s contain the same LOG_LEVEL switch and logger setup. It's also unnecessary: slog.Level can already parse level names (lvl.UnmarshalText([]byte(os.Getenv("LOG_LEVEL")))), case-insensitively.
- A protocol shared by copy and paste. Frame, HEADER_SIZE and the encode/decode logic exist in both services, joined only by the comment "must match encodeFrame in video-feed.go". A comment like that means the code belongs in one shared package.
- Fault injection inside the business logic. sendVideoBytes does three jobs: sending frames, counting drops, and simulating a bad network. The chaos is part of the test environment, not the program.
- Measuring and reporting are combined. streamStats.report takes a logger and logs directly. Its real job is to compute numbers. Who receives them should be decided elsewhere: a log line, a Kafka event or a metric.
- main() and handleConnection do everything. Config, logger, listen, accept loop, reading, stats and forwarding all sit in one place, so you can't test or swap any piece on its own.
- Minor points: the constants are SCREAMING_CASE (Go style is MixedCaps), the logger is passed as a parameter in one service but taken from slog.With in the other, and shutdown is still missing (no context.Context).

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

```
    src/
        cmd/receiver/main.go       # ~15 lines: load config, build, run
        cmd/video-feed/main.go
        internal/platform/         # NewLogger(service), shutdown context
        internal/frame/            # Frame, Encode, Read — the one protocol
        internal/stats/            # Observe(frame), Snapshot() — returns data, never logs
        internal/chaos/            # FaultyWriter wraps any io.Writer
        internal/receiver/         # Server struct: owns the connection loop

```

- You need three things for good logging: logs for humans, metrics, trace. atm the metrics are just being printed out as logs. How production does it: 

Type	Behaves like	Your stats
Counter	Only goes up, never resets	frames_received_total, frames_dropped_total
Gauge	Goes up and down	connections_active
Histogram	Counts values into buckets (≤16 ms, ≤100 ms, ≤1 s, ≤5 s…)	frame_delay_seconds, which replaces avg/max and gives p50/p99
Notice that fps isn't stored at all. The query works it out from the counter: rate(frames_received_total[1m]), meaning "how fast did this counter grow over the last minute". Because the stored data is raw totals, any time window and any statistic can be calculated afterwards.

There are two ways to get the numbers out of the app:

Pull (Prometheus, the most common): the app serves an HTTP endpoint, /metrics, showing the current totals. Prometheus visits it every ~15 s ("scrapes" it) and stores the values. Grafana draws the charts. If the stream stalls, the counter stops growing and fps drops to 0. That's visible and you can alert on it, which fixes the "only reports when a frame arrives" problem.
Push (OpenTelemetry SDK): the app sends metrics to a collector, which forwards them to Prometheus or another backend. It's vendor-neutral, with somewhat more setup.