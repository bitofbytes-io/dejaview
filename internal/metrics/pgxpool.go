package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// PoolStater is implemented by *pgxpool.Pool.
type PoolStater interface {
	Stat() *pgxpool.Stat
}

// PgxPoolCollector exports pgxpool.Stat on each scrape.
type PgxPoolCollector struct {
	pool PoolStater

	acquiredConns        *prometheus.Desc
	idleConns            *prometheus.Desc
	constructingConns    *prometheus.Desc
	totalConns           *prometheus.Desc
	maxConns             *prometheus.Desc
	acquireCount         *prometheus.Desc
	acquireDuration      *prometheus.Desc
	emptyAcquireCount    *prometheus.Desc
	emptyAcquireWait     *prometheus.Desc
	canceledAcquireCount *prometheus.Desc
}

// NewPgxPoolCollector returns a collector for the pool's connection stats.
func NewPgxPoolCollector(pool PoolStater) *PgxPoolCollector {
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc("pgxpool_"+name, help, nil, nil)
	}
	return &PgxPoolCollector{
		pool:                 pool,
		acquiredConns:        desc("acquired_conns", "Connections currently acquired from the pool."),
		idleConns:            desc("idle_conns", "Idle connections in the pool."),
		constructingConns:    desc("constructing_conns", "Connections being constructed."),
		totalConns:           desc("total_conns", "Total connections in the pool."),
		maxConns:             desc("max_conns", "Maximum size of the pool."),
		acquireCount:         desc("acquire_count_total", "Successful acquires from the pool."),
		acquireDuration:      desc("acquire_duration_seconds_total", "Total time spent in successful acquires."),
		emptyAcquireCount:    desc("empty_acquire_count_total", "Successful acquires that waited because the pool was empty."),
		emptyAcquireWait:     desc("empty_acquire_wait_seconds_total", "Total time spent waiting for a connection because the pool was empty."),
		canceledAcquireCount: desc("canceled_acquire_count_total", "Acquires cancelled by a context."),
	}
}

// Describe implements prometheus.Collector.
func (c *PgxPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		c.acquiredConns, c.idleConns, c.constructingConns, c.totalConns, c.maxConns,
		c.acquireCount, c.acquireDuration, c.emptyAcquireCount, c.emptyAcquireWait, c.canceledAcquireCount,
	} {
		ch <- d
	}
}

// Collect implements prometheus.Collector.
func (c *PgxPoolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	gauge := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
	}
	counter := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v)
	}
	gauge(c.acquiredConns, float64(s.AcquiredConns()))
	gauge(c.idleConns, float64(s.IdleConns()))
	gauge(c.constructingConns, float64(s.ConstructingConns()))
	gauge(c.totalConns, float64(s.TotalConns()))
	gauge(c.maxConns, float64(s.MaxConns()))
	counter(c.acquireCount, float64(s.AcquireCount()))
	counter(c.acquireDuration, s.AcquireDuration().Seconds())
	counter(c.emptyAcquireCount, float64(s.EmptyAcquireCount()))
	counter(c.emptyAcquireWait, s.EmptyAcquireWaitTime().Seconds())
	counter(c.canceledAcquireCount, float64(s.CanceledAcquireCount()))
}
