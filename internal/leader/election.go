package leader

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	leaderKey     = "dataserver:leader"
	leaseTTL      = 10 * time.Second // key expires if not renewed
	renewInterval = 3 * time.Second  // how often leader renews
	retryInterval = 3 * time.Second  // how often standby retries
)

// Election manages Redis-based leader election for the data server.
// Only one instance across all dataservers will be active at a time.
//
// Usage:
//
//	e := leader.NewElection(rdb, instanceID)
//	e.Run(ctx,
//	    func(ctx context.Context) { /* became leader — start engines */ },
//	    func()                    { /* lost leadership — stop engines */ },
//	)
type Election struct {
	rdb        *redis.Client
	instanceID string
}

func NewElection(rdb *redis.Client, instanceID string) *Election {
	return &Election{rdb: rdb, instanceID: instanceID}
}

// Run blocks forever, calling onElected when this instance becomes leader
// and onRevoked if it loses the lease (e.g. Redis hiccup causing renewal failure).
// Both callbacks run in separate goroutines managed by Run.
func (e *Election) Run(ctx context.Context, onElected func(ctx context.Context), onRevoked func()) {
	for {
		select {
		case <-ctx.Done():
			e.release(ctx)
			return
		default:
		}

		if e.tryAcquire(ctx) {
			log.Printf("[Leader] instance %s acquired leadership", e.instanceID)
			leaderCtx, cancelLeader := context.WithCancel(ctx)

			// run the engines in background
			go onElected(leaderCtx)

			// renew lease until renewal fails or ctx cancelled
			e.renewLoop(ctx, cancelLeader, onRevoked)

			log.Printf("[Leader] instance %s lost leadership, entering standby", e.instanceID)
		} else {
			log.Printf("[Leader] instance %s is standby, retrying in %s", e.instanceID, retryInterval)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
}

// tryAcquire attempts SET leaderKey instanceID NX EX leaseTTL.
// Returns true only if this instance won the key.
func (e *Election) tryAcquire(ctx context.Context) bool {
	ok, err := e.rdb.SetNX(ctx, leaderKey, e.instanceID, leaseTTL).Result()
	if err != nil {
		log.Printf("[Leader] acquire error: %v", err)
		return false
	}
	return ok
}

// renewLoop keeps refreshing the TTL every renewInterval.
// If renewal fails (Redis down, key missing = someone else took over),
// it cancels the leader context and calls onRevoked.
func (e *Election) renewLoop(ctx context.Context, cancelLeader context.CancelFunc, onRevoked func()) {
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cancelLeader()
			e.release(context.Background())
			return

		case <-ticker.C:
			if !e.renew(ctx) {
				log.Printf("[Leader] instance %s failed to renew lease — revoking", e.instanceID)
				cancelLeader()
				if onRevoked != nil {
					go onRevoked()
				}
				return
			}
		}
	}
}

// renew uses a Lua script to atomically check + extend TTL only if
// we still own the key. Returns false if we no longer own it.
func (e *Election) renew(ctx context.Context) bool {
	// Lua: only EXPIRE if the key value matches our instanceID
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("EXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`
	result, err := e.rdb.Eval(ctx, script, []string{leaderKey}, e.instanceID, int(leaseTTL.Seconds())).Int()
	if err != nil {
		log.Printf("[Leader] renew eval error: %v", err)
		return false
	}
	return result == 1
}

// release deletes the leader key only if we own it (graceful shutdown).
func (e *Election) release(ctx context.Context) {
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`
	e.rdb.Eval(ctx, script, []string{leaderKey}, e.instanceID)
	log.Printf("[Leader] instance %s released leadership key", e.instanceID)
}

// IsLeader checks (non-authoritative, for metrics/logging only) if this
// instance currently holds the key. Do NOT use for control flow — use the
// callback pattern in Run() instead.
func (e *Election) IsLeader(ctx context.Context) bool {
	val, err := e.rdb.Get(ctx, leaderKey).Result()
	if err != nil {
		return false
	}
	return val == e.instanceID
}
