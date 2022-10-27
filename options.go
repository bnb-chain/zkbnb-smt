// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package bsmt

import (
	"github.com/bnb-chain/zkbnb-smt/metrics"
	"github.com/panjf2000/ants/v2"
)

// Option is a function that configures SMT.
type Option func(*BASSparseMerkleTree)

func InitializeVersion(version Version) Option {
	return func(smt *BASSparseMerkleTree) {
		smt.version = version
	}
}

func BatchSizeLimit(limit int) Option {
	return func(smt *BASSparseMerkleTree) {
		smt.batchSizeLimit = limit
	}
}

func DBCacheSize(size int) Option {
	return func(smt *BASSparseMerkleTree) {
		smt.dbCacheSize = size
	}
}

func GoRoutinePool(pool *ants.Pool) Option {
	return func(smt *BASSparseMerkleTree) {
		smt.goroutinePool = pool
	}
}

func GCThreshold(threshold uint64) Option {
	return func(smt *BASSparseMerkleTree) {
		if smt.gcStatus != nil {
			smt.gcStatus.threshold = threshold
			smt.gcStatus.segment = threshold / 10
		}

	}
}

func EnableMetrics(metrics metrics.Metrics) Option {
	return func(smt *BASSparseMerkleTree) {
		smt.metrics = metrics
	}
}
