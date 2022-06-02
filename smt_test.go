package bsmt

import (
	"crypto/sha256"
	"errors"
	"hash"
	"testing"

	"github.com/bnb-chain/bas-smt/database"
	wrappedLevelDB "github.com/bnb-chain/bas-smt/database/leveldb"
	"github.com/bnb-chain/bas-smt/database/memory"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

type testEnv struct {
	hasher hash.Hash
	db     database.TreeDB
}

func prepareEnv(t *testing.T) []testEnv {
	db, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}

	return []testEnv{
		{
			hasher: sha256.New(),
			db:     memory.NewMemoryDB(),
		},
		{
			hasher: sha256.New(),
			db:     wrappedLevelDB.WrapWithNamespace(wrappedLevelDB.NewFromExistLevelDB(db), "test"),
		},
	}
}

func testProof(t *testing.T, hasher hash.Hash, db database.TreeDB) {
	smt, err := NewBASSparseMerkleTree(hasher, db, 50, 128)
	if err != nil {
		t.Fatal(err)
	}

	key1 := uint64(0)
	key2 := uint64(1)
	key3 := uint64(245)
	hasher.Write([]byte("test1"))
	val1 := hasher.Sum(nil)
	hasher.Reset()
	version := smt.LatestVersion()
	_, err = smt.Get(key1, &version)
	if err == nil {
		t.Fatal("tree contains element before write")
	} else if !errors.Is(err, ErrNodeNotFound) {
		t.Fatal(err)
	}

	hasher.Write([]byte("test2"))
	val2 := hasher.Sum(nil)
	hasher.Reset()
	hasher.Write([]byte("test3"))
	val3 := hasher.Sum(nil)
	hasher.Reset()
	smt.Set(key1, val1)
	smt.Set(key2, val2)
	smt.Set(key3, val3)

	version, err = smt.Commit()
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key1, &version)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key2, &version)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key3, &version)
	if err != nil {
		t.Fatal(err)
	}

	proof, err := smt.GetProof(key1, &version)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(proof, &version) {
		t.Fatal("verify proof1 failed")
	}

	proof, err = smt.GetProof(key2, &version)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(proof, &version) {
		t.Fatal("verify proof2 failed")
	}

	proof, err = smt.GetProof(key3, &version)
	if err != nil {
		t.Fatal(err)
	}

	if !smt.VerifyProof(proof, &version) {
		t.Fatal("verify proof3 failed")
	}

	// restore tree from db
	smt2, err := NewBASSparseMerkleTree(sha256.New(), db, 50, 128)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt2.Get(key1, &version)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt2.Get(key2, &version)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt2.Get(key3, &version)
	if err != nil {
		t.Fatal(err)
	}

	proof, err = smt2.GetProof(key1, &version)
	if err != nil {
		t.Fatal(err)
	}

	if !smt2.VerifyProof(proof, &version) {
		t.Fatal("verify proof1 failed")
	}

	proof, err = smt2.GetProof(key2, &version)
	if err != nil {
		t.Fatal(err)
	}

	if !smt2.VerifyProof(proof, &version) {
		t.Fatal("verify proof2 failed")
	}

	proof, err = smt2.GetProof(key3, &version)
	if err != nil {
		t.Fatal(err)
	}

	if !smt2.VerifyProof(proof, &version) {
		t.Fatal("verify proof2 failed")
	}
}

func Test_BASSparseMerkleTree_Proof(t *testing.T) {
	for _, env := range prepareEnv(t) {
		testProof(t, env.hasher, env.db)
		env.db.Close()
	}
}

func testRollback(t *testing.T, hasher hash.Hash, db database.TreeDB) {
	smt, err := NewBASSparseMerkleTree(hasher, db, 50, 128)
	if err != nil {
		t.Fatal(err)
	}

	key1 := uint64(1)
	key2 := uint64(2)
	key3 := uint64(23)
	hasher.Write([]byte("test1"))
	val1 := hasher.Sum(nil)
	hasher.Reset()
	hasher.Write([]byte("test2"))
	val2 := hasher.Sum(nil)
	hasher.Reset()
	hasher.Write([]byte("test3"))
	val3 := hasher.Sum(nil)
	hasher.Reset()
	smt.Set(key1, val1)
	smt.Set(key2, val2)

	version1, err := smt.Commit()
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
	version2, err := smt.Commit()
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key3, &version1)
	if err == nil {
		t.Fatal("get key3 from version1 should be failed")
	}

	_, err = smt.Get(key3, &version2)
	if err != nil {
		t.Fatal(err)
	}

	err = smt.Rollback(version1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = smt.Get(key3, &version1)
	if err == nil {
		t.Fatal("get key3 from version1 should be failed")
	}

	_, err = smt.Get(key3, &version2)
	if !errors.Is(err, ErrVersionTooHigh) {
		t.Fatal(err)
	}

	_, err = smt.GetProof(key3, &version1)
	if err == nil {
		t.Fatal("get key3 proof from version1 should be failed")
	}

	_, err = smt.GetProof(key3, &version2)
	if !errors.Is(err, ErrVersionTooHigh) {
		t.Fatal(err)
	}

	// restore tree from db
	smt2, err := NewBASSparseMerkleTree(sha256.New(), db, 50, 128)
	if err != nil {
		t.Fatal(err)
	}
	_, err = smt2.Get(key3, &version2)
	if !errors.Is(err, ErrVersionTooHigh) {
		t.Fatal(err)
	}

	_, err = smt2.GetProof(key3, &version1)
	if err == nil {
		t.Fatal("get key3 proof from version1 should be failed")
	}

	_, err = smt2.GetProof(key3, &version2)
	if !errors.Is(err, ErrVersionTooHigh) {
		t.Fatal(err)
	}
}

func Test_BASSparseMerkleTree_Rollback(t *testing.T) {
	for _, env := range prepareEnv(t) {
		testRollback(t, env.hasher, env.db)
		env.db.Close()
	}
}

func testReset(t *testing.T, hasher hash.Hash, db database.TreeDB) {
	smt, err := NewBASSparseMerkleTree(hasher, db, 50, 128)
	if err != nil {
		t.Fatal(err)
	}

	key1 := uint64(1)
	key2 := uint64(2)
	key3 := uint64(3)
	hasher.Write([]byte("test1"))
	val1 := hasher.Sum(nil)
	hasher.Reset()
	hasher.Write([]byte("test2"))
	val2 := hasher.Sum(nil)
	hasher.Reset()
	hasher.Write([]byte("test3"))
	val3 := hasher.Sum(nil)
	hasher.Reset()
	smt.Set(key1, val1)
	smt.Set(key2, val2)

	version1, err := smt.Commit()
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

	_, err = smt.Get(key3, &version1)
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatal(err)
	}

	_, err = smt.GetProof(key3, &version1)
	if err == nil {
		t.Fatal("get key3 proof from version1 should be failed")
	}
}

func Test_BASSparseMerkleTree_Reset(t *testing.T) {
	for _, env := range prepareEnv(t) {
		testReset(t, env.hasher, env.db)
		env.db.Close()
	}
}
