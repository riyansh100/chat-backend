package analytics

// Feeder is the interface every analytics engine must satisfy
// to be registered and fed price ticks by the background worker.
//
// Adding a new indicator in the future:
//  1. Implement Input() and InputLen() on your engine — done, it's auto-registered.
//  2. Wire its typed output channel in hub_init.go for broadcast + persistence.
//  3. Add history delivery in hub_run.go on room join.
//
// The feed path (worker → engines) requires zero changes.
type Feeder interface {
	Input() chan<- PriceUpdateEvent
	InputLen() int
}

// Registry holds all registered analytics engines and fans out
// price ticks to all of them in a single call.
type Registry struct {
	engines []Feeder
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds an engine to the registry.
func (r *Registry) Register(e Feeder) {
	r.engines = append(r.engines, e)
}

// Feed pushes a tick into every registered engine, non-blocking.
// If an engine's input channel is full, the tick is dropped for that engine only.
func (r *Registry) Feed(tick PriceUpdateEvent) {
	for _, e := range r.engines {
		select {
		case e.Input() <- tick:
		default:
			// engine backlogged — drop for this engine, never block the worker
		}
	}
}

// InputLens returns a snapshot of each engine's input channel backlog.
// Index order matches registration order.
func (r *Registry) InputLens() []int {
	lens := make([]int, len(r.engines))
	for i, e := range r.engines {
		lens[i] = e.InputLen()
	}
	return lens
}
