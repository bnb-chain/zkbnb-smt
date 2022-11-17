package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/bnb-chain/zkbnb-smt"
	"github.com/bnb-chain/zkbnb-smt/database"
	wrappedLevelDB "github.com/bnb-chain/zkbnb-smt/database/leveldb"
	"github.com/bnb-chain/zkbnb-smt/database/memory"
	wrappedRedis "github.com/bnb-chain/zkbnb-smt/database/redis"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-redis/redis/v8"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"hash"
	"log"
	"net/http"
	_ "net/http/pprof"
	"sync"
	"time"
)

//go tool pprof -http=:8081 /Users/damon/GolandProjects/bnb-chain/zkbnb-smt/democpu.pprof
//go tool pprof -http=:8081 /Users/damon/GolandProjects/bnb-chain/zkbnb-smt/cpu.profile
//go tool pprof -text /Users/damon/GolandProjects/bnb-chain/zkbnb-smt/democpu.pprof

//go tool pprof -http :8877 http://localhost:8081/debug/pprof/profile?seconds=10

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/go", multiUpdateHandler)

	s := &http.Server{
		Addr:    "127.0.0.1:8080",
		Handler: mux,
	}

	go func() {
		log.Fatalln(http.ListenAndServe("127.0.0.1:8081", nil))
	}()

	go func() {
		if err := s.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	done := make(chan bool)
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_, _ = http.Get("http://127.0.0.1:8080/go")
			}
		}
	}()

	time.Sleep(20 * time.Second)
	ticker.Stop()
	done <- true
	fmt.Println("Ticker stopped")
}

func multiUpdateHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("req")
	memEnv := prepareEnv()[0]
	items := []bsmt.Item{
		{Key: 0, Val: memEnv.hasher.Hash([]byte("val0"))},
		{Key: 1, Val: memEnv.hasher.Hash([]byte("val1"))},
	}

	var wg sync.WaitGroup
	wg.Add(200)
	for i := 0; i < 200; i++ {
		go func() {
			defer wg.Done()
			DoMultiUpdate(memEnv, items, 8)
		}()
	}
	wg.Wait()
	w.Write([]byte("success"))
}

func DoMultiUpdate(env testEnv, items []bsmt.Item, depth uint8) {
	//fmt.Printf("test depth %d\n", depth)
	db1, err := env.db()
	if err != nil {
		panic(err)
	}
	//db2, err := env.db()
	//if err != nil {
	//	panic(err)
	//}
	defer db1.Close()
	//defer db2.Close()
	smt1 := NewSMT(env.hasher, db1, depth)
	//smt2 := NewSMT(env.hasher, db2, depth)

	//starT1 := time.Now()
	err = smt1.MultiUpdate(items)
	if err != nil {
		panic(err)
	}
	//tc1 := time.Since(starT1)
	//fmt.Printf("MultiUpdate time cost %v, depth %d, keys %d\n", tc1, depth, len(items))

	_, err = smt1.Commit(nil)
	if err != nil {
		panic(err)
	}

	//starT2 := time.Now()
	//err = smt2.MultiSet(items)
	//if err != nil {
	//	panic(err)
	//}
	//tc2 := time.Since(starT2)
	//fmt.Printf("MultiSet    time cost %v, depth %d, keys %d\n", tc2, depth, len(items))
	//_, err = smt2.Commit(nil)
	//if err != nil {
	//	panic(err)
	//}
}

var nilHash = common.FromHex("01ef55cdf3b9b0d65e6fb6317f79627534d971fd96c811281af618c0028d5e7a")

func NewSMT(hasher *bsmt.Hasher, db database.TreeDB, maxDepth uint8) bsmt.SparseMerkleTree {
	smt, err := bsmt.NewBASSparseMerkleTree(hasher, db, maxDepth, nilHash,
		bsmt.GCThreshold(1024*10))
	if err != nil {
		panic(err)
	}
	return smt
}

type testEnv struct {
	tag    string
	hasher *bsmt.Hasher
	db     func() (database.TreeDB, error)
}

func prepareEnv() []testEnv {
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
			hasher: bsmt.NewHasherPool(func() hash.Hash { return sha256.New() }),
			db:     initMemoryDB,
		},
		{
			tag:    "levelDB",
			hasher: bsmt.NewHasherPool(func() hash.Hash { return sha256.New() }),
			db:     initLevelDB,
		},
		{
			tag:    "redis",
			hasher: bsmt.NewHasherPool(func() hash.Hash { return sha256.New() }),
			db:     initRedisDB,
		},
	}
}
