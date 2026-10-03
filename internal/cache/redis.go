package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Options struct {
	PoolSize     int
	MinIdleConns int
	PoolTimeout  time.Duration
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func OpenRedis(redisURL string, tunings ...Options) (*redis.Client, error) {
	tuning := Options{
		PoolSize:     20,
		MinIdleConns: 5,
		PoolTimeout:  4 * time.Second,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
	if len(tunings) > 0 {
		tuning = tunings[0]
	}

	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	options.PoolSize = tuning.PoolSize
	options.MinIdleConns = tuning.MinIdleConns
	options.PoolTimeout = tuning.PoolTimeout
	options.DialTimeout = tuning.DialTimeout
	options.ReadTimeout = tuning.ReadTimeout
	options.WriteTimeout = tuning.WriteTimeout

	client := redis.NewClient(options)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}
