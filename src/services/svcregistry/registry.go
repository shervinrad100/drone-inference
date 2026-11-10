package main

import (
	"context"
	"drone-target-detection/src/internal/svc"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"
	"uuid"
)

const (
	REGISTRY_UPDATE_LOOP = 1 * time.Second
)

var (
	ErrServiceNotFound  = errors.New("service not found")
	ErrInstanceNotFound = errors.New("instance not found")
)

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
		Instances:   make([]svc.Instance, len(sr.instances)),
	}
	for _, instance := range sr.instances {
		service.Instances = append(service.Instances, instance.info)
	}
	return service
}

type Registry struct {
	services map[string]serviceRecord
}

// Function registers an instance of a service into the registry.
// A service can only exist when there's at least one instance of it
// if new service, add to registry, if new instance add to service list
// if address is already registered, assign new ID and re-register.
// - Old instances will be cleaned up so no worries
func (r *Registry) register(serviceId string, address string) (svc.Instance, error) {
	// TODO accept description what if name matches but description doesn't?

	if _, _, err := net.SplitHostPort(address); err != nil {
		return svc.Instance{}, fmt.Errorf("bad address input: %w", err)
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

	if service, ok := r.services[serviceId]; !ok {
		r.services[serviceId] = serviceRecord{
			name:      serviceId,
			instances: map[uuid.UUID]instanceRecord{instanceId: instance},
		}
	} else {
		service.instances[instanceId] = instance
	}

	slog.Info("New service instance registered", "service.id", serviceId, "service.instance", instance.info)
	return instance.info, nil

}

// Function looks to see if the service name is in the registry
// if it is available it will return a copy of the struct and a bool flag to indicate found
// otherwise returns a nil value Service struct and a false
func (r *Registry) lookup(serviceId string) (svc.Service, bool) {
	service, ok := r.services[serviceId]
	if !ok {
		return svc.Service{}, false
	}
	return service.snapshot(), true
}

// Function returns a list of all registered services
func (r *Registry) list() map[string]svc.Service {
	registry := make(map[string]svc.Service)

	for serviceId, service := range r.services {
		registry[serviceId] = service.snapshot()
	}

	return registry
}

// expires an instance by changing the status using service service ID and instance id
// this allows services to shutdown gracefully
func (r *Registry) deregister(serviceId string, instanceId uuid.UUID) (svc.Instance, error) {
	if service, ok := r.services[serviceId]; ok {
		instance, ok := service.instances[instanceId]
		if ok {
			instance.info.Status = svc.Down
			r.services[serviceId].instances[instanceId] = instance
			return instance.info, nil
		}
		return svc.Instance{}, ErrInstanceNotFound
	}
	return svc.Instance{}, ErrServiceNotFound
}

// updates the last signal from a service instance
// TODO this is an internal function does it need to return instance info? why not just update in place and return nil error
func (r *Registry) renewLease(serviceId string, instanceId uuid.UUID) (svc.Instance, error) {
	if service, ok := r.services[serviceId]; ok {
		if instance, ok := service.instances[instanceId]; ok {
			instance.lastSeen = time.Now()
			r.services[serviceId].instances[instanceId] = instance
			return instance.info, nil
		}
		return svc.Instance{}, ErrInstanceNotFound
	}
	return svc.Instance{}, ErrServiceNotFound
}

func (r *Registry) cleanup() {
	for serviceId, service := range r.services {
		for instanceId, instance := range service.instances {
			if time.Since(instance.lastSeen) > svc.TTL {
				// service is offline
				delete(r.services[serviceId].instances, instanceId)
				slog.Info("instance removed - TTL exceeded", "service.id", serviceId, "service.instance", instance.info)
			} else if instance.info.Status == svc.Down {
				// service has shutdown
				delete(r.services[serviceId].instances, instanceId)
				slog.Info("instance removed - Status == Down", "service.id", serviceId, "service.instance", instance.info)
			}
		}

		// if all instances of the service are offline, remove service
		if len(service.instances) == 0 {
			delete(r.services, serviceId)
			slog.Debug("service removed - no instances", "service.id", serviceId)
		}
	}
}

// the main loop that looks through instances and cleans up any dead instances
func (r *Registry) monitor(ctx context.Context) {
	ticker := time.NewTicker(REGISTRY_UPDATE_LOOP)
	defer ticker.Stop()

	for range ticker.C {
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

// TODO create mutexes so that async reads and writes are safe
// Once you implement mutexes its easier to change to pointer semantics
// so that you can change while holding the lock
