package history

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	chatredis "github.com/riyansh/chat-backend/internal/redis"
)

const (
	TTL1m = 48 * time.Hour
	TTL1h = 7 * 24 * time.Hour
)

// Point is a generic time-value entry stored in Redis sorted sets.
// For multi-field indicators (MACD, BB, OHLC), Value is a JSON string.
type Point struct {
	Ts    int64  // unix seconds — used as score
	Value string // float string for scalar, JSON for multi-field
}

type Store struct {
	rdb     *redis.Client             // write client — primary via sentinel
	readRDB *chatredis.SafeReadClient // read client — replica w/ fallback
	pool    *pgxpool.Pool
}

func NewStore(rdb *redis.Client, readRDB *chatredis.SafeReadClient, pool *pgxpool.Pool) *Store {
	return &Store{rdb: rdb, readRDB: readRDB, pool: pool}
}

// ---- key helpers ----

func Key1m(indicator string, instrumentID int) string {
	return fmt.Sprintf("hist:1m:%s:%d", indicator, instrumentID)
}

func Key1h(indicator string, instrumentID int) string {
	return fmt.Sprintf("hist:1h:%s:%d", indicator, instrumentID)
}

// ---- write (primary) ----

func (s *Store) Write1m(ctx context.Context, indicator string, instrumentID int, ts int64, value string) error {
	key := Key1m(indicator, instrumentID)
	err := s.rdb.ZAdd(ctx, key, redis.Z{Score: float64(ts), Member: value}).Err()
	if err != nil {
		return err
	}
	s.rdb.ZRemRangeByRank(ctx, key, 0, -2881)
	s.rdb.Expire(ctx, key, TTL1m)
	return nil
}

func (s *Store) Write1h(ctx context.Context, indicator string, instrumentID int, ts int64, value string) error {
	key := Key1h(indicator, instrumentID)
	err := s.rdb.ZAdd(ctx, key, redis.Z{Score: float64(ts), Member: value}).Err()
	if err != nil {
		return err
	}
	s.rdb.ZRemRangeByRank(ctx, key, 0, -169)
	s.rdb.Expire(ctx, key, TTL1h)
	return nil
}

// ---- read (replica w/ fallback) ----

func (s *Store) GetLastN(ctx context.Context, indicator string, instrumentID int, resolution string, n int) ([]Point, error) {
	var key string
	if resolution == "1h" {
		key = Key1h(indicator, instrumentID)
	} else {
		key = Key1m(indicator, instrumentID)
	}
	zs, err := s.readRDB.ZRangeWithScores(ctx, key, int64(-n), -1).Result()
	if err != nil {
		return nil, err
	}
	points := make([]Point, 0, len(zs))
	for _, z := range zs {
		points = append(points, Point{Ts: int64(z.Score), Value: fmt.Sprintf("%v", z.Member)})
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
	zs, err := s.readRDB.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatInt(fromUnix, 10),
		Max: strconv.FormatInt(toUnix, 10),
	}).Result()
	if err != nil {
		return nil, err
	}
	points := make([]Point, 0, len(zs))
	for _, z := range zs {
		points = append(points, Point{Ts: int64(z.Score), Value: fmt.Sprintf("%v", z.Member)})
	}
	return points, nil
}

// ---- Postgres fallback ----

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
			points = append(points, Point{Ts: ts, Value: strconv.FormatFloat(val, 'f', 6, 64)})
		case "macd":
			var macdLine, signalLine, histogram float64
			if err := rows.Scan(&ts, &macdLine, &signalLine, &histogram); err != nil {
				continue
			}
			b, _ := json.Marshal(map[string]float64{"macd_line": macdLine, "signal_line": signalLine, "histogram": histogram})
			points = append(points, Point{Ts: ts, Value: string(b)})
		case "bb":
			var upper, middle, lower float64
			if err := rows.Scan(&ts, &upper, &middle, &lower); err != nil {
				continue
			}
			b, _ := json.Marshal(map[string]float64{"upper": upper, "middle": middle, "lower": lower})
			points = append(points, Point{Ts: ts, Value: string(b)})
		case "ohlc":
			var open, high, low, close float64
			if err := rows.Scan(&ts, &open, &high, &low, &close); err != nil {
				continue
			}
			b, _ := json.Marshal(map[string]float64{"open": open, "high": high, "low": low, "close": close})
			points = append(points, Point{Ts: ts, Value: string(b)})
		}
	}
	return points, nil
}

// BackfillRedis writes Postgres points back into Redis (primary) async.
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

	// backfill always goes to primary (write path)
	if err := s.rdb.ZAdd(ctx, key, members...).Err(); err != nil {
		log.Printf("[BackfillRedis] %s:%d error: %v", indicator, instrumentID, err)
		return
	}
	s.rdb.ZRemRangeByRank(ctx, key, 0, -maxEntries-1)
	s.rdb.Expire(ctx, key, ttl)
	log.Printf("[BackfillRedis] %s:%d backfilled %d points into Redis", indicator, instrumentID, len(points))
}
