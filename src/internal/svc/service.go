package svc

import (
	"time"
	"uuid"
)

// if heartbeat returns server error that means registry is down. Need to re-register? continue with heartbeat with backoff or steady retry?
// - "instance not found": the reaper removed it, so register again (HTTP 404).
// - Registry unreachable or a 5xx error: keep the same ID and retry with backoff.

const (
	HEARTBEAT_INTERVAL = 3 * time.Second
	TTL                = 3*HEARTBEAT_INTERVAL + 1*time.Second
)

type Status string

const (
	Live Status = "live"
	Down Status = "down"
)

type Instance struct {
	ID      uuid.UUID `json:"id"`
	Address string    `json:"address"`
	Status  Status    `json:"status"`
}

type Service struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Instances   []Instance `json:"instances"`
}

// Sends a request to svc registry to register a new instance of a service
func RegisterService(serviceId string) {
	// TODO
	// TODO make sure each service then adds the serviceId to their logs
	// TODO do we need to get the address from docker?
	// client caches latest registry when they start

}

func Heartbeat(serviceId string, instanceId uuid.UUID) {
	// TODO
}
