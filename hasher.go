// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package bsmt

import (
	"hash"
	"sync"
)

func NewHasherPool(init func() hash.Hash) *Hasher {
	return &Hasher{
		pool: sync.Pool{
			New: func() interface{} {
				return init()
			},
		},
	}
}

type Hasher struct {
	pool sync.Pool
}

func (h *Hasher) Hash(inputs ...[]byte) []byte {
	hasher := h.pool.Get().(hash.Hash)
	defer h.pool.Put(hasher)
	hasher.Reset()
	for i := range inputs {
		hasher.Write(inputs[i])
	}
	return hasher.Sum(nil)
}
