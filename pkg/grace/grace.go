package grace

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"go.uber.org/zap"
)

// Service graceful service interface.
type Service interface {
	Stop(ctx context.Context) error
}

// Shutdown priorities: lower values are stopped earlier.
const (
	// PriorityDefault is the default shutdown priority (stopped first).
	PriorityDefault = iota
	// PriorityLow is for infrastructure services (databases, caches)
	// stopped after the default group.
	PriorityLow
	// PriorityLowest is for telemetry and similar services stopped last.
	PriorityLowest
)

// Grace service pool for graceful shutdown services.
//
// Services are grouped by priority and stopped in ascending order:
// lower priority groups are stopped first. Within a single priority
// group services are stopped concurrently.
type Grace struct {
	services []serviceItem
	logger   *zap.Logger
}

type serviceItem struct {
	service  Service
	priority int
}

func New(logger *zap.Logger) *Grace {
	return &Grace{
		services: make([]serviceItem, 0),
		logger:   logger,
	}
}

// Add registers a service with the default priority (PriorityDefault).
func (g *Grace) Add(s Service) {
	g.AddPriority(s, PriorityDefault)
}

// AddPriority registers a service with an explicit shutdown priority.
// Lower values are stopped earlier during shutdown.
func (g *Grace) AddPriority(s Service, priority int) {
	g.services = append(g.services, serviceItem{
		service:  s,
		priority: priority,
	})
}

// Stop stops services grouped by priority in ascending order.
// Each priority group is stopped concurrently; the next group starts
// only after the current one finishes or the context expires.
func (g *Grace) Stop(ctx context.Context) {
	if len(g.services) == 0 {
		return
	}

	groups := make(map[int][]Service)
	for _, item := range g.services {
		groups[item.priority] = append(groups[item.priority], item.service)
	}

	priorities := make([]int, 0, len(groups))
	for p := range groups {
		priorities = append(priorities, p)
	}
	sort.Ints(priorities)

	g.logger.Info("Grace: Stopping services",
		zap.Ints("priorities", priorities),
	)

	for _, p := range priorities {
		if ctx.Err() != nil {
			g.logger.Error("Grace: shutdown aborted by context, skipping remaining groups",
				zap.Int("priority", p),
			)

			return
		}

		g.stopGroup(ctx, p, groups[p])
	}

	g.logger.Info("Service pool: All services stoped 🪦")
}

func (g *Grace) stopGroup(ctx context.Context, priority int, services []Service) {
	g.logger.Info("Grace: Stopping priority group",
		zap.Int("priority", priority),
		zap.Int("count", len(services)),
	)

	var wg sync.WaitGroup

	for _, s := range services {
		wg.Add(1)
		go func(s Service) {
			defer wg.Done()

			err := s.Stop(ctx)
			if err != nil {
				g.logger.Error("Grace: stop service error",
					zap.String("service", fmt.Sprintf("%T", s)),
					zap.Int("priority", priority),
					zap.Error(err),
				)

				return
			}

			g.logger.Info("Grace: service stopped",
				zap.String("service", fmt.Sprintf("%T", s)),
				zap.Int("priority", priority),
			)
		}(s)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		g.logger.Info("Grace: priority group stopped", zap.Int("priority", priority))
	case <-ctx.Done():
		g.logger.Error("Grace: priority group stop aborted by context 💀",
			zap.Int("priority", priority),
		)
	}
}
