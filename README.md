## zkbnb-smt

<p>
  <img src="https://img.shields.io/github/actions/workflow/status/bnb-chain/zkbnb-smt/checker.yml?style=flat-square">
  <a href="https://github.com/bnb-chain/zkbnb-smt/blob/master/LICENSE">
    <img src="https://img.shields.io/github/license/globocom/go-buffer?color=blue&style=flat-square">
  </a>
  <img src="https://img.shields.io/github/go-mod/go-version/bnb-chain/zkbnb-smt?style=flat-square">
  <a href="https://pkg.go.dev/github.com/bnb-chain/zkbnb-smt">
    <img src="https://img.shields.io/badge/Go-reference-blue?style=flat-square">
  </a>
</p>

zkbnb-smt is an implementation code library based on the concept of `SparseMerkleTree`, which implements the concepts of data persistence and data compression.

For an overview, see the [design](./docs/design.md).


## Installation
```shell
go get github.com/bnb-chain/zkbnb-smt@latest
```

## Quickstart

```go
package main

import (
	"crypto/sha256"
	"fmt"

	bsmt "github.com/bnb-chain/zkbnb-smt"
	"github.com/bnb-chain/zkbnb-smt/database/memory"
)

func main() {
	db := memory.NewMemoryDB()
	hasher := bsmt.NewHasher(sha256.New())
	nilHash := hasher.Hash([]byte("nilHash"))
	maxDepth := uint8(8)

	smt, err := bsmt.NewBNBSparseMerkleTree(hasher, db, maxDepth, nilHash)
	if err != nil {
		fmt.Println(err)
		return
	}

	key1 := uint64(1)
	key2 := uint64(2)
	key3 := uint64(23)
	val1 := hasher.Hash([]byte("test1"))
	val2 := hasher.Hash([]byte("test2"))
	val3 := hasher.Hash([]byte("test3"))
	smt.Set(key1, val1)
	smt.Set(key2, val2)
	smt.Set(key3, val3)

	version1, err := smt.Commit(nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	_, err = smt.Get(key1, &version1)
	if err != nil {
		fmt.Println(err)
		return
	}

	_, err = smt.Get(key2, &version1)
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = smt.Get(key3, &version1)
	if err != nil {
		fmt.Println(err)
		return
	}

	proof, err := smt.GetProof(key1)
	if err != nil {
		fmt.Println(err)
		return
	}
	if !smt.VerifyProof(key1, proof) {
		fmt.Println("verify proof failed")
		return
	}
}
```