// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package bsmt

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-redis/redis/v8"
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"hash"
	"testing"
	"time"

	"github.com/bnb-chain/zkbnb-smt/database"
	wrappedLevelDB "github.com/bnb-chain/zkbnb-smt/database/leveldb"
	"github.com/bnb-chain/zkbnb-smt/database/memory"
	wrappedRedis "github.com/bnb-chain/zkbnb-smt/database/redis"
)

var (
	nilHash = common.FromHex("01ef55cdf3b9b0d65e6fb6317f79627534d971fd96c811281af618c0028d5e7a")
)

type testEnv struct {
	tag    string
	hasher *Hasher
	db     func() (database.TreeDB, error)
}

func prepareEnv(t *testing.T) []testEnv {
	initLevelDB := func() (database.TreeDB, error) {
		db, err := leveldb.Open(storage.NewMemStorage(), nil)
		if err != nil {
			return nil, err
		}
		return wrappedLevelDB.WrapWithNamespace(wrappedLevelDB.NewFromExistLevelDB(db), "test"), nil
	}
	initRedisDB := func() (database.TreeDB, error) {
		mr, err := miniredis.Run()
		if err != nil {
			return nil, err
		}
		client := redis.NewClient(&redis.Options{
			Addr: mr.Addr(),
		})
		return wrappedRedis.WrapWithNamespace(wrappedRedis.NewFromExistRedisClient(client), "test"), nil
	}
	initMemoryDB := func() (database.TreeDB, error) {
		return memory.NewMemoryDB(), nil
	}

	return []testEnv{
		{
			tag:    "memoryDB",
			hasher: NewHasherPool(func() hash.Hash { return sha256.New() }),
			db:     initMemoryDB,
		},
		{
			tag:    "levelDB",
			hasher: NewHasherPool(func() hash.Hash { return sha256.New() }),
			db:     initLevelDB,
		},
		{
			tag:    "redis",
			hasher: NewHasherPool(func() hash.Hash { return sha256.New() }),
			db:     initRedisDB,
		},
	}
}

func testProof(t *testing.T, hasher *Hasher, dbInitializer func() (database.TreeDB, error)) {
	db, err := dbInitializer()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	smt, err := NewBASSparseMerkleTree(hasher, db, 8, nilHash)
	if err != nil {
		t.Fatal(err)
	}

	emptyProof, err := smt.GetProof(255)
	if err != nil {
		t.Fatal(err)
	}
	if !smt.VerifyProof(0, emptyProof) {
		t.Fatal("verify empty proof failed")
	}

	key1 := uint64(0)
	key2 := uint64(255)
	key3 := uint64(213)
	val1 := hasher.Hash([]byte("test1"))
	version := smt.LatestVersion()
	_, err = smt.Get(key1, &version)
	if err == nil {
		t.Fatal("tree contains element before write")
	} else if !errors.Is(err, ErrEmptyRoot) {
		t.Fatal(err)
	}

	val2 := hasher.Hash([]byte("test2"))
	val3 := hasher.Hash([]byte("test3"))
	smt.Set(key1, val1)
	version1, err := smt.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}
	smt.Set(key2, val2)
	version, err = smt.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}
	smt.Set(key3, val3)
	version, err = smt.Commit(&version1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		// test commit nil data
		version, err = smt.Commit(&version)
		if err != nil {
			t.Fatal(err)
		}
	}

	emptyProof, err = smt.GetProof(44)
	if err != nil {
		t.Fatal(err)
	}
	if !smt.VerifyProof(44, emptyProof) {
		t.Fatal("verify empty proof failed")
	}

	hash1, err := smt.Get(key1, &version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash1, val1) {
		t.Fatalf("not equal to the original, want: %v, got: %v", val1, hash1)
	}

	hash2, err := smt.Get(key2, &version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash2, val2) {
		t.Fatalf("not equal to the original, want: %v, got: %v", val2, hash2)
	}

	hash3, err := smt.Get(key3, &version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash3, val3) {
		t.Fatalf("not equal to the original, want: %v, got: %v", val3, hash3)
	}

	proof, err := smt.GetProof(key1)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(key1, proof) {
		t.Fatal("verify proof1 failed")
	}

	proof, err = smt.GetProof(key2)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(key2, proof) {
		t.Fatal("verify proof2 failed")
	}

	proof, err = smt.GetProof(key3)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(key3, proof) {
		t.Fatal("verify proof3 failed")
	}

	// restore tree from db
	smt2, err := NewBASSparseMerkleTree(hasher, db, 8, nilHash)
	if err != nil {
		t.Fatal(err)
	}

	hash11, err := smt2.Get(key1, &version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash1, hash11) {
		t.Fatalf("not equal to the original, want: %v, got: %v", hash1, hash11)
	}

	hash22, err := smt2.Get(key2, &version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash2, hash22) {
		t.Fatalf("not equal to the original, want: %v, got: %v", hash2, hash22)
	}

	hash33, err := smt2.Get(key3, &version)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash3, hash33) {
		t.Fatalf("not equal to the original, want: %v, got: %v", hash3, hash33)
	}

	proof, err = smt2.GetProof(key1)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(key1, proof) {
		t.Fatal("verify proof1 failed")
	}

	proof, err = smt2.GetProof(key2)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(key2, proof) {
		t.Fatal("verify proof2 failed")
	}

	proof, err = smt2.GetProof(key3)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(key3, proof) {
		t.Fatal("verify proof3 failed")
	}

	key4 := uint64(1)
	val4 := hasher.Hash([]byte("test4"))
	err = smt2.Set(key4, val4)
	if err != nil {
		t.Fatal(err)
	}
	_, err = smt2.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	proof, err = smt2.GetProof(key4)
	if err != nil {
		t.Fatal(err)
	}

	if !smt2.VerifyProof(key4, proof) {
		t.Fatal("verify proof4 failed")
	}
}

func Test_BASSparseMerkleTree_Proof(t *testing.T) {
	for _, env := range prepareEnv(t) {
		t.Logf("test [%s]", env.tag)
		testProof(t, env.hasher, env.db)
	}
}

func testMultiSet(t *testing.T, hasher *Hasher, dbInitializer func() (database.TreeDB, error)) {
	db, err := dbInitializer()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	smt, err := NewBASSparseMerkleTree(hasher, db, 8, nilHash,
		GCThreshold(1024*10))
	if err != nil {
		t.Fatal(err)
	}

	db2, err := dbInitializer()
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	smt2, err := NewBASSparseMerkleTree(hasher, db2, 8, nilHash,
		GCThreshold(1024*10))
	if err != nil {
		t.Fatal(err)
	}

	testKVData := []Item{
		{1, hasher.Hash([]byte("val1"))},
		{2, hasher.Hash([]byte("val2"))},
		{3, hasher.Hash([]byte("val3"))},
		{4, hasher.Hash([]byte("val4"))},
		{5, hasher.Hash([]byte("val5"))},
		{6, hasher.Hash([]byte("val6"))},
		{7, hasher.Hash([]byte("val7"))},
		{8, hasher.Hash([]byte("val8"))},
		{9, hasher.Hash([]byte("val9"))},
		{10, hasher.Hash([]byte("val10"))},
		{11, hasher.Hash([]byte("val11"))},
		{12, hasher.Hash([]byte("val12"))},
		{13, hasher.Hash([]byte("val13"))},
		{14, hasher.Hash([]byte("val14"))},
		{200, hasher.Hash([]byte("val200"))},
		{20, hasher.Hash([]byte("val20"))},
		{21, hasher.Hash([]byte("val21"))},
		{22, hasher.Hash([]byte("val22"))},
		{23, hasher.Hash([]byte("val23"))},
		{24, hasher.Hash([]byte("val24"))},
		{26, hasher.Hash([]byte("val26"))},
		{37, hasher.Hash([]byte("val37"))},
		{255, hasher.Hash([]byte("val255"))},
		{254, hasher.Hash([]byte("val254"))},
		{253, hasher.Hash([]byte("val253"))},
		{252, hasher.Hash([]byte("val252"))},
		{251, hasher.Hash([]byte("val251"))},
		{250, hasher.Hash([]byte("val250"))},
		{249, hasher.Hash([]byte("val249"))},
		{248, hasher.Hash([]byte("val248"))},
		{247, hasher.Hash([]byte("val247"))},
		{15, hasher.Hash([]byte("val15"))},
	}

	t.Log("set data")
	err = smt.MultiSet(testKVData)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range testKVData {
		err := smt2.Set(item.Key, item.Val)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = smt2.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(smt.Root(), smt2.Root()) {
		t.Fatalf("root hash does not match, %x, %x\n", smt.Root(), smt2.Root())
	}

	for _, item := range testKVData {
		val, err := smt.Get(item.Key, nil)
		if err != nil {
			t.Fatal("get key from tree1 failed", item.Key, err)
		}
		val2, err := smt2.Get(item.Key, nil)
		if err != nil {
			t.Fatal("get key from tree2 failed", item.Key, err)
		}
		if !bytes.Equal(val, val2) {
			t.Fatalf("leaf node does not match, %x, %x\n", val, val2)
		}
		if !bytes.Equal(val, item.Val) {
			t.Fatalf("leaf node does not match the origin, %x, %x\n", val, item.Val)
		}

		proof, err := smt.GetProof(item.Key)
		if err != nil {
			t.Fatal("get proof from tree1 failed", item.Key, err)
		}

		if !smt2.VerifyProof(item.Key, proof) {
			t.Fatal("verify proof from tree2 failed")
		}
	}
}

func Test_BASSparseMerkleTree_MultiSet(t *testing.T) {
	for _, env := range prepareEnv(t) {
		t.Logf("test [%s]", env.tag)
		testMultiSet(t, env.hasher, env.db)
	}
}

func testRollback(t *testing.T, hasher *Hasher, dbInitializer func() (database.TreeDB, error)) {
	db, err := dbInitializer()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	smt, err := NewBASSparseMerkleTree(hasher, db, 8, nilHash)
	if err != nil {
		t.Fatal(err)
	}

	key1 := uint64(1)
	key2 := uint64(2)
	key3 := uint64(23)
	val1 := hasher.Hash([]byte("test1"))
	val2 := hasher.Hash([]byte("test2"))
	val3 := hasher.Hash([]byte("test3"))
	smt.Set(key1, val1)
	smt.Set(key2, val2)

	version1, err := smt.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key1, &version1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key2, &version1)
	if err != nil {
		t.Fatal(err)
	}

	proof2, err := smt.GetProof(key2)
	if err != nil {
		t.Fatal(err)
	}
	if !smt.VerifyProof(key2, proof2) {
		t.Fatal("verify proof2 failed")
	}

	smt.Set(key3, val3)
	version2, err := smt.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key3, &version2)
	if err != nil {
		t.Fatal(err)
	}

	err = smt.Rollback(version1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key3, &version2)
	if !errors.Is(err, ErrVersionTooHigh) {
		t.Fatal(err)
	}

	if !smt.VerifyProof(key2, proof2) {
		t.Fatal("verify proof2 after rollback failed")
	}

	// restore tree from db
	smt2, err := NewBASSparseMerkleTree(hasher, db, 8, nilHash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = smt2.Get(key3, &version2)
	if !errors.Is(err, ErrVersionTooHigh) {
		t.Fatal(err)
	}

	if !smt2.VerifyProof(key2, proof2) {
		t.Fatal("verify proof2 after restoring from db failed")
	}
}

func Test_BASSparseMerkleTree_Rollback(t *testing.T) {
	for _, env := range prepareEnv(t) {
		t.Logf("test [%s]", env.tag)
		testRollback(t, env.hasher, env.db)
	}
}

func testRollbackRecovery(t *testing.T, hasher *Hasher, dbInitializer func() (database.TreeDB, error)) {
	db, err := dbInitializer()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db2, err := dbInitializer()
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	smt, err := NewBASSparseMerkleTree(hasher, db, 8, nilHash)
	if err != nil {
		t.Fatal(err)
	}
	smt2, err := NewBASSparseMerkleTree(hasher, db2, 8, nilHash)
	if err != nil {
		t.Fatal(err)
	}

	key1 := uint64(0)
	key2 := uint64(1)
	val1 := hasher.Hash([]byte("test1"))
	val2 := hasher.Hash([]byte("test2"))
	val3 := hasher.Hash([]byte("test3"))
	val4 := hasher.Hash([]byte("test4"))
	smt.Set(key1, val1)
	smt.Set(key2, val2)
	smt2.Set(key1, val1)
	smt2.Set(key2, val2)
	version1, err := smt.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}
	version1, err = smt2.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key1, &version1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key2, &version1)
	if err != nil {
		t.Fatal(err)
	}

	proof2, err := smt.GetProof(key2)
	if err != nil {
		t.Fatal(err)
	}
	if !smt.VerifyProof(key2, proof2) {
		t.Fatal("verify proof2 failed")
	}

	smt.Set(key1, val3)
	smt.Set(key2, val4)
	smt2.Set(key1, val3)
	smt2.Set(key2, val4)
	_, err = smt.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = smt2.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	// restore tree from db
	smt2, err = NewBASSparseMerkleTree(hasher, db2, 8, nilHash)
	if err != nil {
		t.Fatal(err)
	}

	err = smt2.Rollback(version1)
	if err != nil {
		t.Fatal(err)
	}

	err = smt.Rollback(version1)
	if err != nil {
		t.Fatal(err)
	}

	proof2, err = smt2.GetProof(key2)
	if err != nil {
		t.Fatal(err)
	}
	if !smt.VerifyProof(key2, proof2) {
		t.Fatal("[origin] verify proof2 failed")
	}

	proof2, err = smt.GetProof(key2)
	if err != nil {
		t.Fatal(err)
	}
	if !smt2.VerifyProof(key2, proof2) {
		t.Fatal("[recovery] verify proof2 failed")
	}
}

func Test_BASSparseMerkleTree_RollbackAfterRecovery(t *testing.T) {
	for _, env := range prepareEnv(t) {
		t.Logf("test [%s]", env.tag)
		testRollbackRecovery(t, env.hasher, env.db)
	}
}

func testReset(t *testing.T, hasher *Hasher, dbInitializer func() (database.TreeDB, error)) {
	db, err := dbInitializer()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	smt, err := NewBASSparseMerkleTree(hasher, db, 8, nilHash)
	if err != nil {
		t.Fatal(err)
	}

	key1 := uint64(1)
	key2 := uint64(2)
	key3 := uint64(3)
	val1 := hasher.Hash([]byte("test1"))
	val2 := hasher.Hash([]byte("test2"))
	val3 := hasher.Hash([]byte("test3"))
	smt.Set(key1, val1)
	smt.Set(key2, val2)

	version1, err := smt.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key1, &version1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key2, &version1)
	if err != nil {
		t.Fatal(err)
	}

	smt.Set(key3, val3)
	smt.Reset()
}

func Test_BASSparseMerkleTree_Reset(t *testing.T) {
	for _, env := range prepareEnv(t) {
		t.Logf("test [%s]", env.tag)
		testReset(t, env.hasher, env.db)
	}
}

func testGC(t *testing.T, hasher *Hasher, dbInitializer func() (database.TreeDB, error)) {
	db, err := dbInitializer()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	smt, err := NewBASSparseMerkleTree(hasher, db, 8, nilHash,
		GCThreshold(1024*10))
	if err != nil {
		t.Fatal(err)
	}

	testKVData := []struct {
		key uint64
		val []byte
	}{
		{1, hasher.Hash([]byte("val1"))},
		{2, hasher.Hash([]byte("val2"))},
		{3, hasher.Hash([]byte("val3"))},
		{4, hasher.Hash([]byte("val4"))},
		{5, hasher.Hash([]byte("val5"))},
		{6, hasher.Hash([]byte("val6"))},
		{7, hasher.Hash([]byte("val7"))},
		{8, hasher.Hash([]byte("val8"))},
		{9, hasher.Hash([]byte("val9"))},
		{10, hasher.Hash([]byte("val10"))},
		{11, hasher.Hash([]byte("val11"))},
		{12, hasher.Hash([]byte("val12"))},
		{13, hasher.Hash([]byte("val13"))},
		{14, hasher.Hash([]byte("val14"))},
		{200, hasher.Hash([]byte("val200"))},
		{20, hasher.Hash([]byte("val20"))},
		{21, hasher.Hash([]byte("val21"))},
		{22, hasher.Hash([]byte("val22"))},
		{23, hasher.Hash([]byte("val23"))},
		{24, hasher.Hash([]byte("val24"))},
		{26, hasher.Hash([]byte("val26"))},
		{37, hasher.Hash([]byte("val37"))},
		{255, hasher.Hash([]byte("val255"))},
		{254, hasher.Hash([]byte("val254"))},
		{253, hasher.Hash([]byte("val253"))},
		{252, hasher.Hash([]byte("val252"))},
		{251, hasher.Hash([]byte("val251"))},
		{250, hasher.Hash([]byte("val250"))},
		{249, hasher.Hash([]byte("val249"))},
		{248, hasher.Hash([]byte("val248"))},
		{247, hasher.Hash([]byte("val247"))},
		{15, hasher.Hash([]byte("val15"))},
	}

	t.Log("set data")
	for version, testData := range testKVData {
		smt.Set(testData.key, testData.val)
		if version >= 2 {
			pruneVer := Version(version - 1)
			_, err = smt.Commit(&pruneVer)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			_, err = smt.Commit(nil)
			if err != nil {
				t.Fatal(err)
			}
		}

		t.Log("tree.Size() = ", smt.Size())
	}

	t.Log("verify proofs")
	for _, testData := range testKVData {
		proof, err := smt.GetProof(testData.key)
		if err != nil {
			t.Fatal(err)
		}
		if !smt.VerifyProof(testData.key, proof) {
			t.Fatalf("verify proof of key [%d] failed", testData.key)
		}
		t.Log("tree.Size() = ", smt.Size())
	}

	t.Log("test gc")
	smt.Set(0, hasher.Hash([]byte("val0")))
	pruneVer := Version(len(testKVData) - 2)
	_, err = smt.Commit(&pruneVer)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("tree.Size() = ", smt.Size())
	proof, err := smt.GetProof(0)
	if err != nil {
		t.Fatal(err)
	}
	if !smt.VerifyProof(0, proof) {
		t.Fatalf("verify proof of key [%d] failed", 0)
	}

	proof, err = smt.GetProof(200)
	if err != nil {
		t.Fatal(err)
	}
	if !smt.VerifyProof(200, proof) {
		t.Fatalf("verify proof of key [%d] failed", 200)
	}
}

func Test_BASSparseMerkleTree_GC(t *testing.T) {
	for _, env := range prepareEnv(t) {
		t.Logf("test [%s]", env.tag)
		testGC(t, env.hasher, env.db)
	}
}

func Test_BASSparseMerkleTree_MultiUpdate(t *testing.T) {
	rawKvs := map[uint64]string{
		1:   "val1",
		2:   "val2",
		3:   "val3",
		4:   "val4",
		5:   "val5",
		6:   "val6",
		7:   "val7",
		8:   "val8",
		9:   "val9",
		10:  "val10",
		11:  "val11",
		12:  "val12",
		13:  "val13",
		14:  "val14",
		200: "val200",
		20:  "val20",
		21:  "val21",
		22:  "val22",
		23:  "val23",
		24:  "val24",
		26:  "val26",
		37:  "val37",
		255: "val255",
		254: "val254",
		253: "val253",
		252: "val252",
		251: "val251",
		250: "val250",
		249: "val249",
		248: "val248",
		247: "val247",
		15:  "val15",
	}

	depth := []uint8{8, 16, 32}
	for _, env := range prepareEnv(t) {
		t.Logf("test [%s]", env.tag)
		var items []Item
		for k, v := range rawKvs {
			items = append(items, Item{
				Key: k,
				Val: env.hasher.Hash([]byte(v)),
			})
		}
		for _, d := range depth {
			testMultiUpdate(t, env, items, d)
		}
	}
}

func testMultiUpdate(t *testing.T, env testEnv, items []Item, depth uint8) {
	t.Logf("test depth %d", depth)
	db1, err := env.db()
	if err != nil {
		t.Fatal(err)
	}
	db2, err := env.db()
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	defer db2.Close()
	smt1 := newSMT(t, env.hasher, db1, depth)
	smt2 := newSMT(t, env.hasher, db2, depth)

	starT1 := time.Now()
	err = smt1.MultiUpdate(items)
	if err != nil {
		t.Fatal(err)
	}
	tc1 := time.Since(starT1)
	fmt.Printf("MultiUpdate time cost %v, depth %d, keys %d\n", tc1, depth, len(items))

	_, err = smt1.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	starT2 := time.Now()
	err = smt2.MultiSet(items)
	if err != nil {
		t.Fatal(err)
	}
	tc2 := time.Since(starT2)
	fmt.Printf("MultiSet    time cost %v, depth %d, keys %d\n", tc2, depth, len(items))
	_, err = smt2.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	verifyItems(t, smt1, smt2, items)
}

func newSMT(t *testing.T, hasher *Hasher, db database.TreeDB, maxDepth uint8) SparseMerkleTree {
	smt, err := NewBASSparseMerkleTree(hasher, db, maxDepth, nilHash,
		GCThreshold(1024*10))
	if err != nil {
		t.Fatal(err)
	}
	return smt
}

func Test_BASSparseMerkleTree_Set(t *testing.T) {
	for _, env := range prepareEnv(t) {
		t.Logf("test [%s]", env.tag)
		testSet(t, env, 8)
	}
}

func testSet(t *testing.T, env testEnv, depth uint8) {
	t.Logf("test depth %d", depth)
	db1, err := env.db()
	if err != nil {
		t.Fatal(err)
	}
	db2, err := env.db()
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	items := []Item{{201, env.hasher.Hash([]byte("val201"))}}
	smt1 := newSMT(t, env.hasher, db1, depth)
	smt1.Set(items[0].Key, items[0].Val)
	_, err = smt1.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	smt2 := newSMT(t, env.hasher, db1, depth)
	smt2.MultiSet(items)
	_, err = smt2.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	verifyItems(t, smt1, smt2, items)
}

func verifyItems(t *testing.T, smt1 SparseMerkleTree, smt2 SparseMerkleTree, items []Item) {
	if !bytes.Equal(smt1.Root(), smt2.Root()) {
		t.Fatalf("root hash does not match, keys %d, %x, %x\n", len(items), smt1.Root(), smt2.Root())
	}

	for _, item := range items {
		val, err := smt1.Get(item.Key, nil)
		if err != nil {
			t.Fatal("get key from tree1 failed", item.Key, err)
		}
		val2, err := smt2.Get(item.Key, nil)
		if err != nil {
			t.Fatal("get key from tree2 failed", item.Key, err)
		}
		if !bytes.Equal(val, val2) {
			t.Fatalf("leaf node does not match, %x, %x\n", val, val2)
		}
		if !bytes.Equal(val, item.Val) {
			t.Fatalf("leaf node does not match the origin, %x, %x\n", val, item.Val)
		}

		proof, err := smt1.GetProof(item.Key)
		if err != nil {
			t.Fatal("get proof from tree1 failed", item.Key, err)
		}

		if !smt2.VerifyProof(item.Key, proof) {
			t.Fatal("verify proof from tree2 failed")
		}
	}
}
