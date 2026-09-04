package extapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

type registryTestModule struct {
	info       ModuleInfo
	navs       []NavEntry
	initErr    error
	initCount  atomic.Int32
	destroyCnt atomic.Int32
}

func (m *registryTestModule) Info() ModuleInfo          { return m.info }
func (m *registryTestModule) Nav() []NavEntry           { return m.navs }
func (m *registryTestModule) Services() []Service       { return nil }
func (m *registryTestModule) Permissions() []Permission { return nil }
func (m *registryTestModule) Protocol() int             { return 1 }
func (m *registryTestModule) IsInitialized() bool       { return m.initCount.Load() > m.destroyCnt.Load() }
func (m *registryTestModule) OnDestroy() error          { m.destroyCnt.Add(1); return nil }
func (m *registryTestModule) OnInit(context.Context) error {
	m.initCount.Add(1)
	return m.initErr
}

type registryTestStore struct {
	mu      sync.Mutex
	enabled map[string]bool
}

func (s *registryTestStore) IsModuleEnabled(id string, defaultEnabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled, ok := s.enabled[id]; ok {
		return enabled
	}
	return defaultEnabled
}

func (s *registryTestStore) SetModuleEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled[id] = enabled
	return nil
}

func newRegistryTestModule(id string) *registryTestModule {
	return &registryTestModule{info: ModuleInfo{ID: id, Name: id}}
}

func TestRegistryEnsureActiveErrors(t *testing.T) {
	registry := NewRegistry(nil)
	if err := registry.EnsureActive("missing"); !errors.Is(err, ErrUnknownModule) {
		t.Fatalf("EnsureActive unknown error = %v", err)
	}

	store := &registryTestStore{enabled: map[string]bool{"disabled": false}}
	registry = NewRegistry(store)
	module := newRegistryTestModule("disabled")
	if err := registry.Register(module); err != nil {
		t.Fatal(err)
	}
	if err := registry.EnsureActive("disabled"); !errors.Is(err, ErrModuleDisabled) {
		t.Fatalf("EnsureActive disabled error = %v", err)
	}
	if got := module.initCount.Load(); got != 0 {
		t.Fatalf("OnInit count = %d, want 0", got)
	}
}

func TestRegistryConcurrentEnsureActiveInitializesOnce(t *testing.T) {
	registry := NewRegistry(nil)
	module := newRegistryTestModule("module")
	if err := registry.Register(module); err != nil {
		t.Fatal(err)
	}

	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- registry.EnsureActive("module")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := module.initCount.Load(); got != 1 {
		t.Fatalf("OnInit count = %d, want 1", got)
	}
	if !registry.IsActive("module") {
		t.Fatal("module should be active")
	}
}

func TestRegistryInitFailureCanRetry(t *testing.T) {
	registry := NewRegistry(nil)
	module := newRegistryTestModule("module")
	module.initErr = errors.New("boom")
	if err := registry.Register(module); err != nil {
		t.Fatal(err)
	}
	if err := registry.EnsureActive("module"); err == nil {
		t.Fatal("expected init error")
	}
	if registry.IsActive("module") {
		t.Fatal("failed module should remain inactive")
	}
	module.initErr = nil
	if err := registry.EnsureActive("module"); err != nil {
		t.Fatal(err)
	}
	if got := module.initCount.Load(); got != 2 {
		t.Fatalf("OnInit count = %d, want 2", got)
	}
}

func TestRegistryDisableDestroysOnce(t *testing.T) {
	registry := NewRegistry(&registryTestStore{enabled: make(map[string]bool)})
	module := newRegistryTestModule("module")
	if err := registry.Register(module); err != nil {
		t.Fatal(err)
	}
	if err := registry.EnsureActive("module"); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetEnabled("module", false); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetEnabled("module", false); err != nil {
		t.Fatal(err)
	}
	if got := module.destroyCnt.Load(); got != 1 {
		t.Fatalf("OnDestroy count = %d, want 1", got)
	}
	if registry.IsEnabled("module") || registry.IsActive("module") {
		t.Fatal("disabled module should be disabled and inactive")
	}
}

func TestRegistryNavigationOrderIsDeterministic(t *testing.T) {
	registry := NewRegistry(nil)
	for _, tc := range []struct {
		id, route string
	}{
		{id: "z", route: "/z"},
		{id: "a", route: "/a"},
		{id: "m", route: "/m"},
	} {
		module := newRegistryTestModule(tc.id)
		module.navs = []NavEntry{{ID: tc.id, Route: tc.route, Section: SectionExt, Order: 10}}
		if err := registry.Register(module); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 100; i++ {
		navs := registry.GetEnabledNavs()
		got := fmt.Sprintf("%s,%s,%s", navs[0].ID, navs[1].ID, navs[2].ID)
		if got != "a,m,z" {
			t.Fatalf("navigation order = %s, want a,m,z", got)
		}
	}
}

func TestRegistryConcurrentStateAccess(t *testing.T) {
	registry := NewRegistry(&registryTestStore{enabled: make(map[string]bool)})
	module := newRegistryTestModule("module")
	if err := registry.Register(module); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(4)
		go func(enabled bool) {
			defer wg.Done()
			_ = registry.SetEnabled("module", enabled)
		}(i%2 == 0)
		go func() { defer wg.Done(); _ = registry.List() }()
		go func() { defer wg.Done(); _ = registry.GetEnabledNavs() }()
		go func() { defer wg.Done(); _ = registry.IsActive("module") }()
	}
	wg.Wait()
	registry.ShutdownAll()
}
