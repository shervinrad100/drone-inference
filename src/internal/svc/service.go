package svc

import (
	"time"
	"uuid"
)

// TODO if heartbeat returns server error that means registry is down. Need to re-register? continue with heartbeat with backoff or steady retry?
// TODO "instance not found": the reaper removed it, so register again (HTTP 404).
// TODO Registry unreachable or a 5xx error: keep the same ID and retry with backoff.

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

// b) Put the request and response types in internal/svc, not in main.go. svc.RegisterService (the client) has to send exactly the same JSON body.
// If the type lives in package main, the client can't import it, and a second copy will drift. svc.Instance and svc.Service are already in svc, so add RegisterRequest next to them. The path already carries the service ID, so the body should only hold what the path doesn't: address, and later description. If the name is in both, which one wins?

// c) Make errors part of the contract too. Write one function that maps errors to status codes:
// - ErrServiceNotFound and ErrInstanceNotFound → 404.
// - A bad address → 400. But register currently wraps the net error without a sentinel, so a handler can't tell bad input from any other failure. Add ErrInvalidAddress and return fmt.Errorf("%w: %w", ErrInvalidAddress, err).
// - Return errors as JSON {"error": "..."} instead of http.Error's plain text, so the client only has to parse one format.

// Steps for your decode[T] helper:
// 1. Cap the body size: req.Body = http.MaxBytesReader(w, req.Body, 4<<10). If you get a *http.MaxBytesError (check with errors.As), return 413.
// 2. Decode: json.UnmarshalRead(req.Body, &dst, json.RejectUnknownMembers(true)). It reads to EOF, so trailing data fails.
// 3. Classify the error so the message is useful. Everything below is a 400; only the message differs:
//    - errors.Is(err, json.ErrUnknownName) → "unknown field".
//    - *json.SemanticError → use its JSONPointer to name the field with the wrong type.
//    - Anything else → "malformed JSON".
// 4. Check required fields. Neither JSON version has a required tag. Add a Validate() error method to svc.RegisterRequest so the rules sit next to the type. The handler checks that fields are present. The registry still owns what they mean: register keeps its SplitHostPort check, because it can't trust callers.
// 5. Parse path parameters strictly too. Run uuid.Parse(req.PathValue("instanceId")) and return 400 if it fails, before you touch the registry. From then on the registry only ever sees a uuid.UUID, never a raw string.
// 6. Write responses in the right order. Set Content-Type: application/json before WriteHeader, because headers set afterwards are silently dropped. For register, return 201 with Location: /services/{id}/instances/{instanceId}.

type RegisterInstanceRequest struct {
	Address string `json:"address"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
