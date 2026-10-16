package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// knownStatuses are reset to 0 on every pass so a status that drops to zero
// rows stops reporting its last non-zero value.
var knownStatuses = []string{"TODO", "IN_PROGRESS", "DONE"}

// Collector periodically refreshes the gauges that cannot be derived from
// request traffic: pool saturation and entity counts.
type Collector struct {
	db       *pgxpool.Pool
	interval time.Duration
}

func NewCollector(db *pgxpool.Pool, interval time.Duration) *Collector {
	return &Collector{db: db, interval: interval}
}

// Run blocks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.collect(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("metrics collector stopped")
			return
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

func (c *Collector) collect(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stat := c.db.Stat()
	SetDBConnections(float64(stat.AcquiredConns()))

	var users, projects, tasks int64
	const countQuery = `
		SELECT
			(SELECT count(*) FROM users),
			(SELECT count(*) FROM projects),
			(SELECT count(*) FROM tasks)
	`
	if err := c.db.QueryRow(ctx, countQuery).Scan(&users, &projects, &tasks); err != nil {
		slog.WarnContext(ctx, "metrics: count entities", "error", err)
		return
	}

	SetUsersTotal(float64(users))
	SetProjectsTotal(float64(projects))
	SetTasksTotal(float64(tasks))

	rows, err := c.db.Query(ctx, `SELECT status, count(*) FROM tasks GROUP BY status`)
	if err != nil {
		slog.WarnContext(ctx, "metrics: count tasks by status", "error", err)
		return
	}
	defer rows.Close()

	counts := make(map[string]float64, len(knownStatuses))
	for _, status := range knownStatuses {
		counts[status] = 0
	}

	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			slog.WarnContext(ctx, "metrics: scan status count", "error", err)
			return
		}
		counts[status] = float64(count)
	}

	if err := rows.Err(); err != nil {
		slog.WarnContext(ctx, "metrics: iterate status counts", "error", err)
		return
	}

	for status, count := range counts {
		SetTasksByStatus(status, count)
	}
}
