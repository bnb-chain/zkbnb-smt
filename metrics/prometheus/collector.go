package prometheus

import (
	"fmt"

	"github.com/bnb-chain/bas-smt/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var _ metrics.Metrics = (*Collector)(nil)

func NewCollector() *Collector {
	currentVersion := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "smt_current_version",
		Help: "The current version of smt",
	})
	prunedVersion := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "smt_pruned_version",
		Help: "The current pruned version of smt",
	})
	currentSize := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "smt_tree_size",
		Help: "The current size of tree",
	})
	changeSize := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "smt_change_size",
		Help: "The size changed of each commit, rollback",
	})
	commitNum := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "smt_commit_nums",
		Help: "The number of nodes for each commit",
	})
	latestGCVersion := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "smt_latest_gc_version",
		Help: "The version number of the last GC",
	})
	gcThreshold := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "smt_latest_gc_threshold",
		Help: "GC trigger threshold",
	})
	prometheus.MustRegister(
		currentVersion,
		prunedVersion,
		currentSize,
		changeSize,
		commitNum,
		latestGCVersion,
		gcThreshold)

	var (
		gcVersions [10]prometheus.Gauge
		gcSizes    [10]prometheus.Gauge
	)
	for i := 0; i < 10; i++ {
		ver := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("smt_gc_versions_%d", i),
			Help: fmt.Sprintf("GC version of the %dth field value", i),
		})
		gcVersions[i] = ver
		size := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("smt_gc_sizes_%d", i),
			Help: fmt.Sprintf("GC size of the %dth field value", i),
		})
		gcSizes[i] = size
		prometheus.MustRegister(ver, size)
	}

	return &Collector{
		currentVersion:  currentVersion,
		prunedVersion:   prunedVersion,
		currentSize:     currentSize,
		changeSize:      changeSize,
		commitNum:       commitNum,
		latestGCVersion: latestGCVersion,
		gcThreshold:     gcThreshold,
		gcVersions:      gcVersions,
		gcSizes:         gcSizes,
	}
}

type Collector struct {
	currentVersion  prometheus.Gauge
	prunedVersion   prometheus.Gauge
	currentSize     prometheus.Gauge
	changeSize      prometheus.Gauge
	commitNum       prometheus.Gauge
	latestGCVersion prometheus.Gauge
	gcThreshold     prometheus.Gauge
	gcVersions      [10]prometheus.Gauge
	gcSizes         [10]prometheus.Gauge
}

func (c *Collector) Version(ver uint64) {
	c.currentVersion.Set(float64(ver))
}

func (c *Collector) PrunedVersion(ver uint64) {
	c.prunedVersion.Set(float64(ver))
}

func (c *Collector) CurrentSize(size uint64) {
	c.currentSize.Set(float64(size))
}

func (c *Collector) ChangeSize(size uint64) {
	c.changeSize.Set(float64(size))
}

func (c *Collector) CommitNum(n int) {
	c.commitNum.Set(float64(n))
}

func (c *Collector) LatestGCVersion(ver uint64) {
	c.latestGCVersion.Set(float64(ver))
}

func (c *Collector) GCThreshold(threshold uint64) {
	c.gcThreshold.Set(float64(threshold))
}

func (c *Collector) GCVersions(info [10]*metrics.GCVersion) {
	for i := range info {
		c.gcVersions[i].Set(float64(info[i].Version))
		c.gcSizes[i].Set(float64(info[i].Size))
	}
}
