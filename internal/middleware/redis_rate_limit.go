package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"go-simple-task-api/internal/observability"
	"go-simple-task-api/pkg/response"
)

type AuthRateLimiter interface {
	Handler(http.Handler) http.Handler
}

type RedisRateLimiter struct {
	client   *redis.Client
	limit    int64
	window   time.Duration
	failOpen bool
	logger   *slog.Logger
	metrics  *observability.Metrics
}

func NewRedisRateLimiter(
	client *redis.Client,
	limit int,
	window time.Duration,
	failOpen bool,
	logger *slog.Logger,
	metrics *observability.Metrics,
) *RedisRateLimiter {
	return &RedisRateLimiter{
		client:   client,
		limit:    int64(limit),
		window:   window,
		failOpen: failOpen,
		logger:   logger,
		metrics:  metrics,
	}
}

func (l *RedisRateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, err := l.allow(r.Context(), clientIP(r))
		if err != nil {
			l.logger.ErrorContext(
				r.Context(),
				"rate_limit_redis_error",
				"request_id", RequestIDFromContext(r.Context()),
				"error", err,
			)
			if l.failOpen {
				next.ServeHTTP(w, r)
				return
			}
			response.JSON(w, http.StatusServiceUnavailable, response.Envelope{
				Success: false,
				Message: "rate limiter unavailable",
			})
			return
		}
		if !allowed {
			l.metrics.IncRateLimited()
			response.JSON(w, http.StatusTooManyRequests, response.Envelope{
				Success: false,
				Message: "too many requests",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *RedisRateLimiter) allow(ctx context.Context, key string) (bool, error) {
	windowMS := l.window.Milliseconds()
	redisKey := fmt.Sprintf(
		"task-api:auth-rate:%s:%d",
		key,
		time.Now().UnixMilli()/windowMS,
	)

	script := "local current = redis.call('INCR', KEYS[1])\n" +
		"if current == 1 then\n" +
		"  redis.call('PEXPIRE', KEYS[1], ARGV[1])\n" +
		"end\n" +
		"return current"

	result, err := l.client.Eval(
		ctx,
		script,
		[]string{redisKey},
		windowMS,
	).Int64()
	if err != nil {
		return false, err
	}

	return result <= l.limit, nil
}
