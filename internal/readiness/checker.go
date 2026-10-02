package readiness

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"go-simple-task-api/internal/observability"
	"go-simple-task-api/pkg/response"
)

type Checker struct {
	db      *sql.DB
	redis   *redis.Client
	timeout time.Duration
	metrics *observability.Metrics
}

func New(db *sql.DB, redisClient *redis.Client, timeout time.Duration, metrics *observability.Metrics) *Checker {
	return &Checker{
		db:      db,
		redis:   redisClient,
		timeout: timeout,
		metrics: metrics,
	}
}

func (c *Checker) Handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{
			Success: false,
			Message: "method not allowed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), c.timeout)
	defer cancel()

	if c.db != nil {
		if err := c.db.PingContext(ctx); err != nil {
			c.metrics.ObserveReadiness(false)
			response.JSON(w, http.StatusServiceUnavailable, response.Envelope{
				Success: false,
				Message: "database is not ready",
			})
			return
		}
	}

	if c.redis != nil {
		if err := c.redis.Ping(ctx).Err(); err != nil {
			c.metrics.ObserveReadiness(false)
			response.JSON(w, http.StatusServiceUnavailable, response.Envelope{
				Success: false,
				Message: "redis is not ready",
			})
			return
		}
	}

	c.metrics.ObserveReadiness(true)
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true,
		Message: "API is ready",
	})
}
