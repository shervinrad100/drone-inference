package main

import (
	"context"
	"drone-target-detection/src/internal/svc"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"uuid"
)

// unit tests aim to make sure the logic works as intended

// helpers

func mustRegister(t *testing.T, r *Registry, serviceId, address string) svc.Instance {
	t.Helper()
	instance, err := r.register(serviceId, address)
	if err != nil {
		t.Fatalf("register(%q, %q) failed: %v", serviceId, address, err)
	}
	return instance
}

// finds an instance through lookup so tests don't depend on the internal maps
func lookupInstance(r *Registry, serviceId string, instanceId uuid.UUID) (svc.Instance, bool) {
	service, ok := r.lookup(serviceId)
	if !ok {
		return svc.Instance{}, false
	}
	i := slices.IndexFunc(service.Instances, func(in svc.Instance) bool { return in.ID == instanceId })
	if i == -1 {
		return svc.Instance{}, false
	}
	return service.Instances[i], true
}

// register

func TestRegister(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		testServiceId := "video-receiver"
		testAddress := "123.123.123.123:8080"

		r := newRegistry()

		gotInstance, err := r.register(testServiceId, testAddress)
		if err != nil {
			t.Fatalf("failed to register service: %v", err)
		}

		if gotInstance.ID == uuid.Nil() || gotInstance.Address != testAddress || gotInstance.Status != svc.Live {
			t.Errorf("instance not created correctly, want a new ID, address %q and status %q, got %+v",
				testAddress, svc.Live, gotInstance)
		}

		service, ok := r.services[testServiceId]
		if !ok {
			t.Fatalf("service was not created, expected %v\nregistry: %v\n", testServiceId, r.services)
		}
		if service.name != testServiceId {
			t.Errorf("service name = %q, want %q", service.name, testServiceId)
		}

		record, ok := service.instances[gotInstance.ID]
		if !ok {
			t.Fatalf("instance %v not stored under its ID, instances: %v", gotInstance.ID, service.instances)
		}
		if record.info != gotInstance {
			t.Errorf("stored instance = %+v, returned %+v", record.info, gotInstance)
		}
		// time is frozen inside the synctest bubble, so both timestamps must equal now exactly
		now := time.Now()
		if !record.lastSeen.Equal(now) || !record.startTime.Equal(now) {
			t.Errorf("lastSeen = %v, startTime = %v, want both %v", record.lastSeen, record.startTime, now)
		}
	})
}

func TestRegisterAddsInstanceToExistingService(t *testing.T) {
	r := newRegistry()

	first := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
	second := mustRegister(t, &r, "video-receiver", "10.0.0.2:8080")

	if first.ID == second.ID {
		t.Fatalf("both instances got the same ID %v", first.ID)
	}
	if n := len(r.services); n != 1 {
		t.Errorf("registry has %d services, want 1", n)
	}
	service, _ := r.lookup("video-receiver")
	if n := len(service.Instances); n != 2 {
		t.Fatalf("service has %d instances, want 2", n)
	}
	for _, want := range []svc.Instance{first, second} {
		if got, ok := lookupInstance(&r, "video-receiver", want.ID); !ok || got != want {
			t.Errorf("instance %v: got %+v (found=%v), want %+v", want.ID, got, ok, want)
		}
	}
}

func TestRegisterSameAddressReplacesInstance(t *testing.T) {
	r := newRegistry()
	other := mustRegister(t, &r, "video-receiver", "10.0.0.2:8080")

	first := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
	second := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

	if first.ID == second.ID {
		t.Fatalf("re-registering an address reused ID %v, want a new one", first.ID)
	}
	if _, ok := lookupInstance(&r, "video-receiver", first.ID); ok {
		t.Error("old instance at the same address is still registered")
	}
	for _, want := range []svc.Instance{second, other} {
		if got, ok := lookupInstance(&r, "video-receiver", want.ID); !ok || got != want {
			t.Errorf("instance %v: got %+v (found=%v), want %+v", want.ID, got, ok, want)
		}
	}
	// a heartbeat with the replaced ID gets "not found", so its sender would register again
	if err := r.renewLease("video-receiver", first.ID); !errors.Is(err, ErrInstanceNotFound) {
		t.Errorf("renewLease with the replaced ID: error = %v, want %v", err, ErrInstanceNotFound)
	}
}

func TestRegisterKeepsServicesSeparate(t *testing.T) {
	r := newRegistry()

	receiver := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
	inference := mustRegister(t, &r, "inference", "10.0.0.2:9000")

	if _, ok := lookupInstance(&r, "video-receiver", inference.ID); ok {
		t.Error("inference instance leaked into video-receiver")
	}
	if _, ok := lookupInstance(&r, "inference", receiver.ID); ok {
		t.Error("video-receiver instance leaked into inference")
	}
}

func TestRegisterAddressValidation(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{"ipv4", "10.0.0.1:8080", false},
		{"hostname", "localhost:8080", false},
		{"ipv6", "[::1]:8080", false},
		{"empty", "", true},
		{"missing port", "10.0.0.1", true},
		{"unbracketed ipv6", "::1:8080", true},
		{"two ports", "10.0.0.1:8080:9090", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRegistry()

			_, err := r.register("video-receiver", tt.address)

			if (err != nil) != tt.wantErr {
				t.Fatalf("register(%q) error = %v, wantErr %v", tt.address, err, tt.wantErr)
			}
			// a rejected address must not leave a service behind
			if _, ok := r.lookup("video-receiver"); ok == tt.wantErr {
				t.Errorf("service registered = %v after error = %v", ok, err)
			}
		})
	}
}

// lookup

func TestLookupUnknownService(t *testing.T) {
	r := newRegistry()
	mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

	got, ok := r.lookup("inference")

	if ok {
		t.Errorf("lookup of unregistered service returned ok, service: %+v", got)
	}
	if got.Name != "" || got.Description != "" || got.Instances != nil {
		t.Errorf("want zero-value Service, got %+v", got)
	}
}

func TestLookup(t *testing.T) {
	testServiceId := "video-receiver"

	r := newRegistry()
	first := mustRegister(t, &r, testServiceId, "123.123.123.123:8080")
	second := mustRegister(t, &r, testServiceId, "123.123.123.124:8080")

	gotService, ok := r.lookup(testServiceId)
	if !ok {
		t.Fatalf("service %q not found after registering it", testServiceId)
	}

	if gotService.Name != testServiceId {
		t.Errorf("name = %q, want %q", gotService.Name, testServiceId)
	}
	if len(gotService.Instances) != 2 {
		t.Fatalf("service has %d instances, want 2: %+v", len(gotService.Instances), gotService.Instances)
	}
	// instances come from a map so their order is random: search, don't index
	for _, want := range []svc.Instance{first, second} {
		if !slices.Contains(gotService.Instances, want) {
			t.Errorf("instance %+v missing from lookup result %+v", want, gotService.Instances)
		}
	}
}

func TestLookupReturnsSnapshot(t *testing.T) {
	r := newRegistry()
	instance := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

	// Service is returned by value, so only the Instances slice could share memory with the registry
	got, _ := r.lookup("video-receiver")
	got.Instances[0].Status = svc.Down

	if stored, _ := lookupInstance(&r, "video-receiver", instance.ID); stored.Status != svc.Live {
		t.Errorf("changing a returned instance changed the registry: status %q", stored.Status)
	}
}

// list

func TestListEmptyRegistry(t *testing.T) {
	r := newRegistry()

	got := r.list()

	// a nil map encodes to JSON null, an empty one to {}
	if got == nil {
		t.Error("list returned a nil map, want an empty one")
	}
	if len(got) != 0 {
		t.Errorf("list returned %d services, want 0", len(got))
	}
}

func TestListReturnsAllServices(t *testing.T) {
	r := newRegistry()
	mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
	mustRegister(t, &r, "video-receiver", "10.0.0.2:8080")
	mustRegister(t, &r, "inference", "10.0.0.3:9000")

	got := r.list()

	want := map[string]int{"video-receiver": 2, "inference": 1}
	if len(got) != len(want) {
		t.Fatalf("list returned %d services, want %d: %+v", len(got), len(want), got)
	}
	for name, instances := range want {
		service, ok := got[name]
		if !ok {
			t.Errorf("service %q missing from list", name)
			continue
		}
		if service.Name != name {
			t.Errorf("list[%q].Name = %q", name, service.Name)
		}
		if len(service.Instances) != instances {
			t.Errorf("list[%q] has %d instances, want %d", name, len(service.Instances), instances)
		}
	}
}

func TestListReturnsSnapshot(t *testing.T) {
	r := newRegistry()
	mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

	got := r.list()
	delete(got, "video-receiver")
	got["inference"] = svc.Service{Name: "inference"}

	if _, ok := r.lookup("video-receiver"); !ok {
		t.Error("deleting from the returned map removed the service from the registry")
	}
	if _, ok := r.lookup("inference"); ok {
		t.Error("adding to the returned map added a service to the registry")
	}
}

// snapshot

func TestSnapshotWithoutInstances(t *testing.T) {
	record := serviceRecord{
		name:        "video-receiver",
		description: "receives drone video",
		instances:   map[uuid.UUID]instanceRecord{},
	}

	got := record.snapshot()

	if got.Name != record.name || got.Description != record.description {
		t.Errorf("snapshot = %+v, want name %q and description %q", got, record.name, record.description)
	}
	// a nil slice encodes to JSON null, an empty one to []
	if got.Instances == nil || len(got.Instances) != 0 {
		t.Errorf("instances = %#v, want an empty non-nil slice", got.Instances)
	}
}

// deregister

func TestDeregisterErrors(t *testing.T) {
	r := newRegistry()
	mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

	tests := []struct {
		name       string
		serviceId  string
		instanceId uuid.UUID
		wantErr    error
	}{
		{"unknown service", "inference", uuid.New(), ErrServiceNotFound},
		{"unknown instance", "video-receiver", uuid.New(), ErrInstanceNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.deregister(tt.serviceId, tt.instanceId)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
			if got != (svc.Instance{}) {
				t.Errorf("returned instance = %+v, want zero value", got)
			}
		})
	}
}

func TestDeregisterMarksInstanceDown(t *testing.T) {
	r := newRegistry()
	leaving := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
	staying := mustRegister(t, &r, "video-receiver", "10.0.0.2:8080")

	got, err := r.deregister("video-receiver", leaving.ID)
	if err != nil {
		t.Fatalf("deregister failed: %v", err)
	}

	want := leaving
	want.Status = svc.Down
	if got != want {
		t.Errorf("deregister returned %+v, want %+v", got, want)
	}
	// the instance stays visible as Down until cleanup removes it
	if stored, ok := lookupInstance(&r, "video-receiver", leaving.ID); !ok || stored.Status != svc.Down {
		t.Errorf("stored instance = %+v (found=%v), want status %q", stored, ok, svc.Down)
	}
	if stored, _ := lookupInstance(&r, "video-receiver", staying.ID); stored.Status != svc.Live {
		t.Errorf("other instance status = %q, want %q", stored.Status, svc.Live)
	}
}

// renewLease

func TestRenewLeaseErrors(t *testing.T) {
	r := newRegistry()
	mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

	tests := []struct {
		name       string
		serviceId  string
		instanceId uuid.UUID
		wantErr    error
	}{
		{"unknown service", "inference", uuid.New(), ErrServiceNotFound},
		{"unknown instance", "video-receiver", uuid.New(), ErrInstanceNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.renewLease(tt.serviceId, tt.instanceId)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRenewLeaseUpdatesLastSeen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRegistry()
		instance := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
		startTime := r.services["video-receiver"].instances[instance.ID].startTime

		time.Sleep(svc.HEARTBEAT_INTERVAL)
		if err := r.renewLease("video-receiver", instance.ID); err != nil {
			t.Fatalf("renewLease failed: %v", err)
		}

		record := r.services["video-receiver"].instances[instance.ID]
		if now := time.Now(); !record.lastSeen.Equal(now) {
			t.Errorf("lastSeen = %v, want %v", record.lastSeen, now)
		}
		if !record.startTime.Equal(startTime) {
			t.Errorf("startTime changed from %v to %v", startTime, record.startTime)
		}
		if record.info != instance {
			t.Errorf("instance info changed from %+v to %+v", instance, record.info)
		}
	})
}

func TestRenewLeaseRejectsDownInstance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRegistry()
		instance := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
		if _, err := r.deregister("video-receiver", instance.ID); err != nil {
			t.Fatalf("deregister failed: %v", err)
		}
		lastSeen := r.services["video-receiver"].instances[instance.ID].lastSeen

		// a heartbeat that lands after deregister but before cleanup removes the instance
		time.Sleep(svc.HEARTBEAT_INTERVAL)
		err := r.renewLease("video-receiver", instance.ID)

		if !errors.Is(err, ErrInstanceDown) {
			t.Errorf("error = %v, want %v", err, ErrInstanceDown)
		}
		// a rejected heartbeat must not revive the instance or extend its lease
		record := r.services["video-receiver"].instances[instance.ID]
		if record.info.Status != svc.Down {
			t.Errorf("status = %q after a rejected heartbeat, want %q", record.info.Status, svc.Down)
		}
		if !record.lastSeen.Equal(lastSeen) {
			t.Errorf("lastSeen moved from %v to %v, want it unchanged", lastSeen, record.lastSeen)
		}
	})
}

// cleanup

func TestCleanupTTLBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRegistry()
		instance := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

		time.Sleep(svc.TTL)
		r.cleanup()
		if _, ok := lookupInstance(&r, "video-receiver", instance.ID); !ok {
			t.Fatal("instance removed at exactly TTL, want it kept until TTL is exceeded")
		}

		time.Sleep(time.Nanosecond)
		r.cleanup()
		if _, ok := r.lookup("video-receiver"); ok {
			t.Error("service still registered after its only instance exceeded TTL")
		}
	})
}

func TestCleanupRemovesOnlyExpiredInstances(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRegistry()
		old := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
		time.Sleep(svc.TTL / 2)
		fresh := mustRegister(t, &r, "video-receiver", "10.0.0.2:8080")
		time.Sleep(svc.TTL/2 + time.Nanosecond)

		r.cleanup()

		if _, ok := lookupInstance(&r, "video-receiver", old.ID); ok {
			t.Error("expired instance was not removed")
		}
		if _, ok := lookupInstance(&r, "video-receiver", fresh.ID); !ok {
			t.Error("instance within TTL was removed")
		}
	})
}

func TestCleanupKeepsInstanceWithRenewedLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRegistry()
		instance := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

		// heartbeat for several TTLs; the instance must never be removed
		for elapsed := time.Duration(0); elapsed < 3*svc.TTL; elapsed += svc.HEARTBEAT_INTERVAL {
			time.Sleep(svc.HEARTBEAT_INTERVAL)
			r.cleanup()
			if err := r.renewLease("video-receiver", instance.ID); err != nil {
				t.Fatalf("renewLease after %v failed: %v", elapsed+svc.HEARTBEAT_INTERVAL, err)
			}
		}
	})
}

func TestCleanupRemovesDownInstance(t *testing.T) {
	r := newRegistry()
	down := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
	live := mustRegister(t, &r, "video-receiver", "10.0.0.2:8080")
	if _, err := r.deregister("video-receiver", down.ID); err != nil {
		t.Fatalf("deregister failed: %v", err)
	}

	r.cleanup()

	if _, ok := lookupInstance(&r, "video-receiver", down.ID); ok {
		t.Error("down instance was not removed")
	}
	if _, ok := lookupInstance(&r, "video-receiver", live.ID); !ok {
		t.Error("live instance was removed")
	}
}

func TestCleanupRemovesServiceWithNoInstances(t *testing.T) {
	r := newRegistry()
	instance := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
	mustRegister(t, &r, "inference", "10.0.0.2:9000")
	if _, err := r.deregister("video-receiver", instance.ID); err != nil {
		t.Fatalf("deregister failed: %v", err)
	}

	r.cleanup()

	if _, ok := r.lookup("video-receiver"); ok {
		t.Error("service with no instances left was not removed")
	}
	if _, ok := r.lookup("inference"); !ok {
		t.Error("unrelated service was removed")
	}
}

// monitor

func TestMonitorCleansUpEveryTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRegistry()
		instance := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")
		if _, err := r.deregister("video-receiver", instance.ID); err != nil {
			t.Fatalf("deregister failed: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go r.monitor(ctx)

		// half-tick offsets stop the test waking at the same instant as the ticker
		time.Sleep(REGISTRY_UPDATE_LOOP / 2)
		if _, ok := r.lookup("video-receiver"); !ok {
			t.Fatal("instance removed before the first tick")
		}

		time.Sleep(REGISTRY_UPDATE_LOOP)
		if _, ok := r.lookup("video-receiver"); ok {
			t.Error("down instance still registered after the first tick")
		}
	})
}

func TestMonitorExpiresInstanceThatStopsHeartbeating(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRegistry()
		instance := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go r.monitor(ctx)

		time.Sleep(REGISTRY_UPDATE_LOOP / 2)
		for range 5 {
			time.Sleep(svc.HEARTBEAT_INTERVAL)
			if err := r.renewLease("video-receiver", instance.ID); err != nil {
				t.Fatalf("heartbeat rejected while the instance should be alive: %v", err)
			}
		}

		// stop heartbeating; give the monitor TTL plus a couple of ticks to notice
		time.Sleep(svc.TTL + 2*REGISTRY_UPDATE_LOOP)
		if _, ok := r.lookup("video-receiver"); ok {
			t.Error("instance still registered after missing heartbeats for longer than TTL")
		}
		if err := r.renewLease("video-receiver", instance.ID); err == nil {
			t.Error("heartbeat from an expired instance was accepted, want an error so it re-registers")
		}
	})
}

func TestMonitorStopsWhenContextCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRegistry()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			r.monitor(ctx)
			close(done)
		}()

		synctest.Wait() // monitor is idle, waiting for a tick
		cancel()
		synctest.Wait()

		select {
		case <-done:
		default:
			t.Error("monitor still running after its context was cancelled")
		}
	})
}

// concurrency: run with -race, every method must hold the lock while it touches the maps

func TestConcurrentAccess(t *testing.T) {
	r := newRegistry()
	anchor := mustRegister(t, &r, "video-receiver", "10.0.0.1:8080")

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			// one address per goroutine, so goroutines don't replace each other's instances
			address := fmt.Sprintf("10.0.1.%d:8080", g)
			for range 50 {
				instance, err := r.register("video-receiver", address)
				if err != nil {
					t.Errorf("register failed: %v", err)
					return
				}
				if err := r.renewLease("video-receiver", anchor.ID); err != nil {
					t.Errorf("renewLease failed: %v", err)
				}
				r.lookup("video-receiver")
				r.list()
				if _, err := r.deregister("video-receiver", instance.ID); err != nil {
					t.Errorf("deregister failed: %v", err)
				}
				r.cleanup()
			}
		})
	}
	wg.Wait()

	service, _ := r.lookup("video-receiver")
	if len(service.Instances) != 1 || service.Instances[0].ID != anchor.ID {
		t.Errorf("instances = %+v, want only the anchor %v left", service.Instances, anchor.ID)
	}
}
