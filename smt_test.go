package bsmt

import (
	"crypto/sha256"
	"testing"

	"github.com/bnb-chain/bas-smt/database/memory"
)

func TestBASSparseMerkleTree(t *testing.T) {
	hasher := sha256.New()
	db := memory.NewMemoryDB()
	smt, err := NewBASSparseMerkleTree(hasher, db, 50, 128)
	if err != nil {
		t.Fatal(err)
	}
	key1 := []byte{0, 0, 0, 1}
	key2 := []byte{1, 1, 1, 0}
	val1 := []byte("test1")
	val2 := []byte("test2")
	smt.Set(key1, val1)
	smt.Set(key2, val2)

	version, err := smt.Commit()
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
}
