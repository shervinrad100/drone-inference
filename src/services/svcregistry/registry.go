package main

import (
	"context"
	"drone-target-detection/src/internal/svc"
	"errors"
	"log/slog"
	"net"
	"regexp"
	"sync"
	"time"
	"uuid"
)

const (
	REGISTRY_UPDATE_LOOP = 1 * time.Second
)

var (
	ErrServiceNotFound    = errors.New("service not found")
	ErrInstanceNotFound   = errors.New("instance not found")
	ErrInstanceDown       = errors.New("instance is down")
	ErrInvalidAddress     = errors.New("bad address input")
	ErrInvalidServiceName = errors.New("invalid service name")
)

var serviceNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validServiceName(name string) bool {
	return len(name) <= 63 && serviceNameRe.MatchString(name)
}

type instanceRecord struct {
	info      svc.Instance
	lastSeen  time.Time
	startTime time.Time
}

type serviceRecord struct {
	name        string
	description string
	instances   map[uuid.UUID]instanceRecord
}

// returns a snapshot of that the service looks like at the time
func (sr *serviceRecord) snapshot() svc.Service {
	service := svc.Service{
		Name:        sr.name,
		Description: sr.description,
		Instances:   make([]svc.Instance, 0, len(sr.instances)),
	}
	for _, instance := range sr.instances {
		service.Instances = append(service.Instances, instance.info)
	}
	return service
}

type Registry struct {
	mu       sync.RWMutex
	services map[string]*serviceRecord
}

func newRegistry() Registry {
	return Registry{services: make(map[string]*serviceRecord)}
}

// Function registers an instance of a service into the registry.
// A service can only exist when there's at least one instance of it
// if new service, add to registry, if new instance add to service list
// if address is already registered, assign new ID and re-register and remove the old instance
func (r *Registry) register(serviceId string, address string) (svc.Instance, error) {
	// TODO accept description what if name matches but description doesn't?

	if _, _, err := net.SplitHostPort(address); err != nil {
		return svc.Instance{}, ErrInvalidAddress
	}

	if !validServiceName(serviceId) {
		return svc.Instance{}, ErrInvalidServiceName
	}

	instanceId := uuid.New()
	instance := instanceRecord{
		info: svc.Instance{
			ID:      instanceId,
			Address: address,
			Status:  svc.Live,
		},
		lastSeen:  time.Now(),
		startTime: time.Now(),
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	service, ok := r.services[serviceId]
	if !ok {
		r.services[serviceId] = &serviceRecord{
			name:      serviceId,
			instances: map[uuid.UUID]instanceRecord{instanceId: instance},
		}
	} else {
		// if the same address is already registered, we need to de-register it
		for id, existing := range service.instances {
			if existing.info.Address == address {
				delete(service.instances, id)
				slog.Warn("instance with same address exists - deleting old instance", "serviceId", serviceId, "instance", existing.info)
			}
		}
		service.instances[instanceId] = instance
	}

	slog.Info("instance registered", "serviceId", serviceId, "instance", instance.info)
	return instance.info, nil

}

// Function looks to see if the service name is in the registry
// if it is available it will return a copy of the struct and a bool flag to indicate found
// otherwise returns a nil value Service struct and a false
func (r *Registry) lookup(serviceId string) (svc.Service, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	service, ok := r.services[serviceId]
	if !ok {
		return svc.Service{}, false
	}
	return service.snapshot(), true
}

// Function returns a list of all registered services
func (r *Registry) list() map[string]svc.Service {
	registry := make(map[string]svc.Service)

	r.mu.RLock()
	defer r.mu.RUnlock()

	for serviceId, service := range r.services {
		registry[serviceId] = service.snapshot()
	}

	return registry
}

// expires an instance by changing the status using the service id and instance id
// this allows services to shutdown gracefully
func (r *Registry) deregister(serviceId string, instanceId uuid.UUID) (svc.Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if service, ok := r.services[serviceId]; ok {
		instance, ok := service.instances[instanceId]
		if ok {
			instance.info.Status = svc.Down
			service.instances[instanceId] = instance
			slog.Info("deregistered instance", "serviceId", serviceId, "instance", instance.info)
			return instance.info, nil
		}
		return svc.Instance{}, ErrInstanceNotFound
	}
	return svc.Instance{}, ErrServiceNotFound
}

// updates the last signal from a service instance
// if a service has de-registered (Status == svc.Down) it must re-register. A heartbeat will be rejected
func (r *Registry) renewLease(serviceId string, instanceId uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if service, ok := r.services[serviceId]; ok {
		if instance, ok := service.instances[instanceId]; ok {
			if instance.info.Status == svc.Down {
				return ErrInstanceDown
			}
			instance.lastSeen = time.Now()
			service.instances[instanceId] = instance
			return nil
		}
		return ErrInstanceNotFound
	}
	return ErrServiceNotFound
}

func (r *Registry) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for serviceId, service := range r.services {
		for instanceId, instance := range service.instances {
			if time.Since(instance.lastSeen) > svc.TTL {
				// service is offline
				delete(service.instances, instanceId)
				slog.Info("instance removed - TTL exceeded", "serviceId", serviceId, "instance", instance.info)
			} else if instance.info.Status == svc.Down {
				// service has shutdown
				delete(service.instances, instanceId)
				slog.Info("instance removed - Status == Down", "serviceId", serviceId, "instance", instance.info)
			}
		}

		// if all instances of the service are offline, remove service
		if len(service.instances) == 0 {
			delete(r.services, serviceId)
			slog.Debug("service removed - no instances", "serviceId", serviceId)
		}
	}
}

// the main loop that looks through instances and cleans up any dead instances
func (r *Registry) monitor(ctx context.Context) {
	ticker := time.NewTicker(REGISTRY_UPDATE_LOOP)
	defer ticker.Stop()

	for {
		// TODO optimise leetcode style, we are doing O(services*instances) with this loop
		select {
		case <-ctx.Done():
			slog.Info("stopping monitor", "reason", context.Cause(ctx))
			return
		case <-ticker.C:
			r.cleanup()
		}
	}

}
