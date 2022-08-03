package metrics

type Metrics interface {
	// The current version of smt
	Version(uint64)
	// The current pruned version of smt
	PrunedVersion(uint64)
	// The current size of tree
	CurrentSize(uint64)
	// The size changed of each commit, rollback
	ChangeSize(uint64)
	// The number of nodes for each commit
	CommitNum(int)
	// The version number of the last GC
	LatestGCVersion(uint64)
	// GC trigger threshold
	GCThreshold(uint64)
	// GC info for each field value
	GCVersions([10]*GCVersion)
}

type GCVersion struct {
	Version uint64
	Size    uint64
}
