// internal/history/store.go
package history

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	bincod "github.com/riyansh/chat-backend/internal/binary"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

const (
	TTL1m = 48 * time.Hour
	TTL1h = 7 * 24 * time.Hour
)

// Point is a single history entry.
// Value is a raw binary member — decode with bincod.DecodeScalar / DecodeOHLC
// etc. before serialising to JSON for the frontend.
type Point struct {
	Ts    int64
	Value []byte // binary-encoded payload (8 / 24 / 32 bytes depending on indicator)
}

type Store struct {
	lb   *chatredis.RedisLoadBalancer
	pool *pgxpool.Pool
}

func NewStore(lb *chatredis.RedisLoadBalancer, pool *pgxpool.Pool) *Store {
	return &Store{lb: lb, pool: pool}
}

func Key1m(indicator string, instrumentID int) string {
	return fmt.Sprintf("hist:1m:%s:%d", indicator, instrumentID)
}

func Key1h(indicator string, instrumentID int) string {
	return fmt.Sprintf("hist:1h:%s:%d", indicator, instrumentID)
}

// ---- write (least-loaded primary) ----

// Write1m stores a binary-encoded value at the given unix-second timestamp.
// value must be the output of one of the bincod.Encode* functions.
func (s *Store) Write1m(ctx context.Context, indicator string, instrumentID int, ts int64, value []byte) error {
	key := Key1m(indicator, instrumentID)
	rdb := s.lb.WriteClient()
	if err := rdb.ZAdd(ctx, key, redis.Z{Score: float64(ts), Member: value}).Err(); err != nil {
		return err
	}
	rdb.ZRemRangeByRank(ctx, key, 0, -2881)
	rdb.Expire(ctx, key, TTL1m)
	return nil
}

func (s *Store) Write1h(ctx context.Context, indicator string, instrumentID int, ts int64, value []byte) error {
	key := Key1h(indicator, instrumentID)
	rdb := s.lb.WriteClient()
	if err := rdb.ZAdd(ctx, key, redis.Z{Score: float64(ts), Member: value}).Err(); err != nil {
		return err
	}
	rdb.ZRemRangeByRank(ctx, key, 0, -169)
	rdb.Expire(ctx, key, TTL1h)
	return nil
}

// ---- read (scatter-gather across replicas) ----

// zToPoint converts a redis.Z whose Member is a binary []byte (from a
// go-redis RESP3 reply) or string (RESP2 legacy) into a Point.
// go-redis v9 returns interface{} — handle both.
func zToPoint(z redis.Z) Point {
	ts := int64(z.Score)
	switch v := z.Member.(type) {
	case []byte:
		return Point{Ts: ts, Value: v}
	case string:
		return Point{Ts: ts, Value: []byte(v)}
	default:
		return Point{Ts: ts, Value: []byte(fmt.Sprintf("%v", v))}
	}
}

func (s *Store) GetLastN(ctx context.Context, indicator string, instrumentID int, resolution string, n int) ([]Point, error) {
	var key string
	if resolution == "1h" {
		key = Key1h(indicator, instrumentID)
	} else {
		key = Key1m(indicator, instrumentID)
	}
	zs, err := s.lb.ZRangeWithScores(ctx, key, int64(-n), -1).Result()
	if err != nil {
		return nil, err
	}
	points := make([]Point, 0, len(zs))
	for _, z := range zs {
		points = append(points, zToPoint(z))
	}
	return points, nil
}

func (s *Store) GetRange(ctx context.Context, indicator string, instrumentID int, resolution string, fromUnix, toUnix int64) ([]Point, error) {
	var key string
	if resolution == "1h" {
		key = Key1h(indicator, instrumentID)
	} else {
		key = Key1m(indicator, instrumentID)
	}
	zs, err := s.lb.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
	if err != nil {
		return nil, err
	}
	points := make([]Point, 0, len(zs))
	for _, z := range zs {
		points = append(points, zToPoint(z))
	}
	return points, nil
}

// ---- Postgres fallback ----

// FallbackFromPostgres reads from Postgres and returns Points with binary
// members so BackfillRedis can write them straight back.
func (s *Store) FallbackFromPostgres(ctx context.Context, indicator string, instrumentID int, fromUnix, toUnix int64) ([]Point, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("no postgres pool")
	}
	from := time.Unix(fromUnix, 0).UTC()
	to := time.Unix(toUnix, 0).UTC()

	var query string
	switch indicator {
	case "sma", "ema", "rsi":
		query = fmt.Sprintf(
			`SELECT EXTRACT(EPOCH FROM time)::bigint, value FROM %s
			 WHERE instrument=$1 AND time >= $2 AND time <= $3 ORDER BY time ASC`, indicator)
	case "macd":
		query = `SELECT EXTRACT(EPOCH FROM time)::bigint, macd_line, signal_line, histogram FROM macd
			 WHERE instrument=$1 AND time >= $2 AND time <= $3 ORDER BY time ASC`
	case "bb":
		query = `SELECT EXTRACT(EPOCH FROM time)::bigint, upper, middle, lower FROM bb
			 WHERE instrument=$1 AND time >= $2 AND time <= $3 ORDER BY time ASC`
	case "ohlc":
		query = `SELECT EXTRACT(EPOCH FROM time)::bigint, open, high, low, close FROM ohlc
			 WHERE instrument=$1 AND time >= $2 AND time <= $3 ORDER BY time ASC`
	default:
		return nil, fmt.Errorf("unknown indicator: %s", indicator)
	}

	rows, err := s.pool.Query(ctx, query, instrumentID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []Point
	for rows.Next() {
		var ts int64
		switch indicator {
		case "sma", "ema", "rsi":
			var val float64
			if err := rows.Scan(&ts, &val); err != nil {
				continue
			}
			points = append(points, Point{Ts: ts, Value: bincod.EncodeScalar(val)})
		case "macd":
			var macdLine, signalLine, histogram float64
			if err := rows.Scan(&ts, &macdLine, &signalLine, &histogram); err != nil {
				continue
			}
			points = append(points, Point{Ts: ts, Value: bincod.EncodeMACD(macdLine, signalLine, histogram)})
		case "bb":
			var upper, middle, lower float64
			if err := rows.Scan(&ts, &upper, &middle, &lower); err != nil {
				continue
			}
			points = append(points, Point{Ts: ts, Value: bincod.EncodeBB(upper, middle, lower)})
		case "ohlc":
			var open, high, low, close float64
			if err := rows.Scan(&ts, &open, &high, &low, &close); err != nil {
				continue
			}
			points = append(points, Point{Ts: ts, Value: bincod.EncodeOHLC(open, high, low, close)})
		}
	}
	return points, nil
}

// BackfillRedis writes Postgres points back to the least-loaded primary.
// Points must already carry binary members (as returned by FallbackFromPostgres).
func (s *Store) BackfillRedis(indicator string, instrumentID int, resolution string, points []Point) {
	ctx := context.Background()
	var key string
	var ttl time.Duration
	var maxEntries int64

	if resolution == "1h" {
		key = Key1h(indicator, instrumentID)
		ttl = TTL1h
		maxEntries = 168
	} else {
		key = Key1m(indicator, instrumentID)
		ttl = TTL1m
		maxEntries = 2880
	}

	members := make([]redis.Z, 0, len(points))
	for _, p := range points {
		members = append(members, redis.Z{Score: float64(p.Ts), Member: p.Value})
	}

	rdb := s.lb.WriteClient()
	if err := rdb.ZAdd(ctx, key, members...).Err(); err != nil {
		log.Printf("[BackfillRedis] %s:%d error: %v", indicator, instrumentID, err)
		return
	}
	rdb.ZRemRangeByRank(ctx, key, 0, -maxEntries-1)
	rdb.Expire(ctx, key, ttl)
	log.Printf("[BackfillRedis] %s:%d backfilled %d points", indicator, instrumentID, len(points))
}
