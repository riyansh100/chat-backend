// internal/redis/loadbalancer.go
//
// RedisLoadBalancer implements three mechanisms your mentor asked for:
//
//  1. LOAD BALANCER   — tracks in-flight requests per node, routes writes
//                       to the least-loaded healthy primary.
//
//  2. SENTINEL        — each pair has its own sentinel watching it.
//                       The write clients are sentinel-aware (NewFailoverClient)
//                       so they auto-follow promoted replicas on failover.
//
//  3. CONCURRENT      — read path fires ZRange/ZRangeByScore to BOTH replicas
//     RETRIEVAL         simultaneously; whichever responds first wins.
//                       If both fail, falls back to the least-loaded primary.
//
// Topology (2 pairs):
//
//	pair1: primary :6381  replica :6380   sentinel :26380  name "mymaster"
//	pair2: primary :6383  replica :6382   sentinel :26381  name "mymaster2"

package redis

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// ---- node: one Redis instance ----

type node struct {
	client   *redis.Client
	addr     string
	inFlight atomic.Int64 // current in-flight requests
	avgLatUs atomic.Int64 // exponential moving avg latency in microseconds
	healthy  atomic.Bool
}

func newNode(client *redis.Client, addr string) *node {
	n := &node{client: client, addr: addr}
	n.healthy.Store(true)
	n.avgLatUs.Store(1000) // seed at 1ms
	return n
}

// recordLatency updates the EMA latency for this node.
// EMA formula: new = 0.2*sample + 0.8*old
func (n *node) recordLatency(us int64) {
	old := n.avgLatUs.Load()
	updated := (us*2 + old*8) / 10
	n.avgLatUs.Store(updated)
}

// score returns a routing score — lower is better.
// Combines in-flight count and avg latency.
func (n *node) score() int64 {
	return n.inFlight.Load()*1000 + n.avgLatUs.Load()
}

// ---- pair: one primary + one replica ----

type redisPair struct {
	primary *node // write target — sentinel-aware
	replica *node // read target — direct
	name    string
}

// ---- RedisLoadBalancer ----

// RedisLoadBalancer manages 2 pairs of Redis (primary+replica).
// It provides:
//   - Least-loaded write routing across primaries
//   - Concurrent scatter-gather reads across replicas (fastest wins)
//   - Health monitoring with automatic node exclusion
//   - Fallback to primary if replica is unhealthy
type RedisLoadBalancer struct {
	pairs []*redisPair
}

// NewRedisLoadBalancer creates the load balancer with 2 pairs.
// Call this once in main, pass to all stores.
//
// pair1Primary/Replica: sentinel client for pair1 primary, direct client for pair1 replica
// pair2Primary/Replica: same for pair2
func NewRedisLoadBalancer(
	pair1Primary *redis.Client, pair1Replica *redis.Client,
	pair2Primary *redis.Client, pair2Replica *redis.Client,
) *RedisLoadBalancer {
	lb := &RedisLoadBalancer{
		pairs: []*redisPair{
			{
				name:    "pair1",
				primary: newNode(pair1Primary, "primary1(:6381)"),
				replica: newNode(pair1Replica, "replica1(:6380)"),
			},
			{
				name:    "pair2",
				primary: newNode(pair2Primary, "primary2(:6383)"),
				replica: newNode(pair2Replica, "replica2(:6382)"),
			},
		},
	}

	// start background health checker
	go lb.healthLoop()

	return lb
}

// ---- health monitoring ----

func (lb *RedisLoadBalancer) healthLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, pair := range lb.pairs {
			lb.checkNode(pair.primary)
			lb.checkNode(pair.replica)
		}
	}
}

func (lb *RedisLoadBalancer) checkNode(n *node) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := n.client.Ping(ctx).Err()
	latUs := time.Since(start).Microseconds()

	if err != nil {
		if n.healthy.Load() {
			log.Printf("[LoadBalancer] node %s went DOWN: %v", n.addr, err)
		}
		n.healthy.Store(false)
	} else {
		if !n.healthy.Load() {
			log.Printf("[LoadBalancer] node %s came UP (latency=%dµs)", n.addr, latUs)
		}
		n.healthy.Store(true)
		n.recordLatency(latUs)
	}
}

// ---- write routing (least-loaded primary) ----

// WriteClient returns the least-loaded healthy primary client.
// Falls back to any healthy primary if all are saturated.
// Returns nil only if ALL primaries are down (caller should handle).
func (lb *RedisLoadBalancer) WriteClient() *redis.Client {
	var best *node

	for _, pair := range lb.pairs {
		p := pair.primary
		if !p.healthy.Load() {
			continue
		}
		if best == nil || p.score() < best.score() {
			best = p
		}
	}

	if best == nil {
		log.Println("[LoadBalancer] WARNING: all primaries unhealthy, trying any...")
		// last resort — return first primary regardless
		return lb.pairs[0].primary.client
	}

	return best.client
}

// ---- concurrent scatter-gather reads ----

// ZRangeWithScores fires the read to ALL healthy replicas concurrently.
// Returns the first successful result. Falls back to WriteClient on total failure.
func (lb *RedisLoadBalancer) ZRangeWithScores(ctx context.Context, key string, start, stop int64) *redis.ZSliceCmd {
	return lb.scatterGatherZRange(ctx, func(c *redis.Client) *redis.ZSliceCmd {
		return c.ZRangeWithScores(ctx, key, start, stop)
	})
}

// ZRangeByScoreWithScores fires the read to ALL healthy replicas concurrently.
func (lb *RedisLoadBalancer) ZRangeByScoreWithScores(ctx context.Context, key string, opt *redis.ZRangeBy) *redis.ZSliceCmd {
	return lb.scatterGatherZRange(ctx, func(c *redis.Client) *redis.ZSliceCmd {
		return c.ZRangeByScoreWithScores(ctx, key, opt)
	})
}

// Get fires to ALL healthy replicas concurrently, returns first success.
func (lb *RedisLoadBalancer) Get(ctx context.Context, key string) *redis.StringCmd {
	type result struct {
		cmd *redis.StringCmd
	}
	ch := make(chan result, len(lb.pairs))

	healthy := 0
	for _, pair := range lb.pairs {
		r := pair.replica
		if !r.healthy.Load() {
			continue
		}
		healthy++
		go func(n *node) {
			start := time.Now()
			n.inFlight.Add(1)
			cmd := n.client.Get(ctx, key)
			n.inFlight.Add(-1)
			n.recordLatency(time.Since(start).Microseconds())
			ch <- result{cmd: cmd}
		}(r)
	}

	if healthy == 0 {
		log.Println("[LoadBalancer] no healthy replicas for Get, falling back to primary")
		return lb.WriteClient().Get(ctx, key)
	}

	// collect results — return first non-error (or last result if all error)
	for i := 0; i < healthy; i++ {
		res := <-ch
		if res.cmd.Err() == nil || res.cmd.Err() == redis.Nil {
			return res.cmd
		}
	}

	// all replicas failed — fallback to primary
	log.Println("[LoadBalancer] all replicas failed for Get, falling back to primary")
	return lb.WriteClient().Get(ctx, key)
}

// scatterGatherZRange is the core concurrent read implementation.
// Fires the read op to all healthy replicas in parallel.
// Returns first successful ZSliceCmd. Falls back to primary if all replicas fail.
func (lb *RedisLoadBalancer) scatterGatherZRange(
	ctx context.Context,
	op func(*redis.Client) *redis.ZSliceCmd,
) *redis.ZSliceCmd {
	type result struct {
		cmd  *redis.ZSliceCmd
		node *node
	}

	ch := make(chan result, len(lb.pairs))

	healthy := 0
	for _, pair := range lb.pairs {
		r := pair.replica
		if !r.healthy.Load() {
			continue
		}
		healthy++
		go func(n *node) {
			start := time.Now()
			n.inFlight.Add(1)
			cmd := op(n.client)
			n.inFlight.Add(-1)
			n.recordLatency(time.Since(start).Microseconds())
			ch <- result{cmd: cmd, node: n}
		}(r)
	}

	if healthy == 0 {
		log.Println("[LoadBalancer] no healthy replicas, falling back to primary for read")
		return op(lb.WriteClient())
	}

	// first successful response wins — drain remaining in background
	for i := 0; i < healthy; i++ {
		res := <-ch
		if res.cmd.Err() == nil || res.cmd.Err() == redis.Nil {
			return res.cmd
		}
	}

	// all replicas errored — fallback to primary
	log.Println("[LoadBalancer] all replicas failed ZRange, falling back to primary")
	return op(lb.WriteClient())
}

// ---- status logging (call periodically or on demand) ----

func (lb *RedisLoadBalancer) LogStatus() {
	for _, pair := range lb.pairs {
		log.Printf("[LoadBalancer] %s | primary %s healthy=%v inFlight=%d avgLat=%dµs | replica %s healthy=%v inFlight=%d avgLat=%dµs",
			pair.name,
			pair.primary.addr, pair.primary.healthy.Load(),
			pair.primary.inFlight.Load(), pair.primary.avgLatUs.Load(),
			pair.replica.addr, pair.replica.healthy.Load(),
			pair.replica.inFlight.Load(), pair.replica.avgLatUs.Load(),
		)
	}
}
