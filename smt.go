// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package bsmt

import (
	"bytes"
	"encoding/binary"
	"sync"

	"github.com/ethereum/go-ethereum/rlp"
	lru "github.com/hashicorp/golang-lru"
	"github.com/panjf2000/ants/v2"
	sysMemory "github.com/pbnjay/memory"
	"github.com/pkg/errors"

	"github.com/bnb-chain/zkbnb-smt/database"
	"github.com/bnb-chain/zkbnb-smt/database/memory"
	"github.com/bnb-chain/zkbnb-smt/metrics"
	"github.com/bnb-chain/zkbnb-smt/utils"
)

var (
	latestVersionKey          = []byte(`latestVersion`)
	recentVersionNumberKey    = []byte(`recentVersionNumber`)
	storageFullTreeNodePrefix = []byte(`t`)
	sep                       = []byte(`:`)
)

// Encode key, format: t:${depth}:${path}
func storageFullTreeNodeKey(depth uint8, path uint64) []byte {
	pathBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(pathBuf, path)
	return bytes.Join([][]byte{storageFullTreeNodePrefix, {depth}, pathBuf}, sep)
}

var _ SparseMerkleTree = (*BASSparseMerkleTree)(nil)

func NewBASSparseMerkleTree(hasher *Hasher, db database.TreeDB, maxDepth uint8, nilHash []byte,
	opts ...Option) (SparseMerkleTree, error) {

	if maxDepth == 0 || maxDepth%4 != 0 {
		return nil, ErrInvalidDepth
	}

	smt := &BASSparseMerkleTree{
		maxDepth:       maxDepth,
		journal:        newJournal(),
		nilHashes:      constructNilHashes(maxDepth, nilHash, hasher),
		hasher:         hasher,
		batchSizeLimit: 100 * 1024,
		dbCacheSize:    2048,
		gcStatus: &gcStatus{
			threshold: sysMemory.TotalMemory() / 8,
			segment:   sysMemory.TotalMemory() / 8 / 10,
		},
	}

	for _, opt := range opts {
		opt(smt)
	}

	if db == nil {
		smt.db = memory.NewMemoryDB()
		smt.root = NewTreeNode(0, 0, smt.nilHashes, smt.hasher)
		return smt, nil
	}

	smt.db = db
	err := smt.initFromStorage()
	if err != nil {
		return nil, err
	}
	smt.lastSaveRoot = smt.root

	if smt.metrics != nil {
		smt.metrics.GCThreshold(smt.gcStatus.threshold)
	}

	smt.dbCache, err = lru.New(smt.dbCacheSize)
	if err != nil {
		return nil, err
	}

	if smt.goroutinePool == nil {
		smt.goroutinePool, err = ants.NewPool(128)
		if err != nil {
			return nil, err
		}
	}

	return smt, nil
}

func constructNilHashes(maxDepth uint8, nilHash []byte, hasher *Hasher) *nilHashes {
	hashes := make([][]byte, maxDepth+1)
	hashes[maxDepth] = nilHash
	for i := 1; i <= int(maxDepth); i++ {
		nHash := hasher.Hash(nilHash, nilHash)
		hashes[maxDepth-uint8(i)] = nHash
		nilHash = nHash
	}
	return &nilHashes{hashes}
}

type nilHashes struct {
	hashes [][]byte
}

func (h *nilHashes) Get(depth uint8) []byte {
	if len(h.hashes)-1 < int(depth) {
		return nil
	}
	return h.hashes[depth]
}

type journalKey struct {
	depth uint8
	path  uint64
}

func newJournal() *journal {
	return &journal{
		data: make(map[journalKey]*TreeNode),
	}
}

type journal struct {
	mu   sync.RWMutex
	data map[journalKey]*TreeNode
}

func (j *journal) get(key journalKey) (*TreeNode, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	node, exist := j.data[key]
	return node, exist
}

func (j *journal) set(key journalKey, val *TreeNode) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.data[key] = val
}

func (j *journal) len() int {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return len(j.data)
}

func (j *journal) flush() {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.data = make(map[journalKey]*TreeNode)
}

func (j *journal) iterate(callback func(key journalKey, val *TreeNode) error) error {
	j.mu.RLock()
	defer j.mu.RUnlock()
	for key, val := range j.data {
		err := callback(key, val)
		if err != nil {
			return err
		}
	}
	return nil
}

// status for GC.
// In the Commit() stage, the version and releasable size will be recorded,
// the size of the current version tree exceeds the threshold and starts to trigger GC.
// The recorded size of the version is divided into 10 stages of threshold,
// each 10% is a partition, and if it exceeds 100%, it is recorded in the last partition.
// When the GC is triggered, the collection will start from the minimum collection size.
type gcStatus struct {
	versions        [10]Version
	sizes           [10]uint64
	threshold       uint64
	segment         uint64
	latestGCVersion Version
}

func (stat *gcStatus) add(version Version, size uint64) {
	if version == 0 || size == 0 {
		return
	}
	index := (size / stat.segment)
	if index > 9 {
		index = 9
	}
	stat.sizes[index] = size
	stat.versions[index] = version
}

func (stat *gcStatus) pop(currentSize uint64) Version {
	if currentSize < stat.threshold {
		return 0
	}

	var (
		except, maximal Version
	)
	for i := 0; i < len(stat.sizes); i++ {
		if stat.sizes[i] > 0 {
			maximal = stat.versions[i]
		}
		if except == 0 && currentSize-stat.sizes[i] < stat.threshold {
			except = stat.versions[i]
			stat.clean(i)
			break
		}
	}

	if except > 0 {
		stat.latestGCVersion = except
		return except
	}
	stat.clean(9)
	stat.latestGCVersion = maximal
	return maximal
}

func (stat *gcStatus) clean(index int) {
	for i := 0; i <= index; i++ {
		stat.sizes[i] = 0
		stat.versions[i] = 0
	}
}

type BASSparseMerkleTree struct {
	version          Version
	recentVersion    Version
	root             *TreeNode
	rootSize         uint64
	lastSaveRoot     *TreeNode
	lastSaveRootSize uint64
	journal          *journal
	maxDepth         uint8
	nilHashes        *nilHashes
	hasher           *Hasher
	db               database.TreeDB
	dbCacheSize      int
	dbCache          *lru.Cache
	batchSizeLimit   int
	gcStatus         *gcStatus
	goroutinePool    *ants.Pool
	metrics          metrics.Metrics
}

func (tree *BASSparseMerkleTree) initFromStorage() error {
	tree.root = NewTreeNode(0, 0, tree.nilHashes, tree.hasher)
	// recovery version info
	buf, err := tree.db.Get(latestVersionKey)
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(buf) > 0 {
		tree.version = Version(binary.BigEndian.Uint64(buf))
	}

	buf, err = tree.db.Get(recentVersionNumberKey)
	if err != nil && !errors.Is(err, database.ErrDatabaseNotFound) {
		return err
	}
	if len(buf) > 0 {
		tree.recentVersion = Version(binary.BigEndian.Uint64(buf))
	}

	// recovery root node from storage
	rlpBytes, err := tree.db.Get(storageFullTreeNodeKey(0, 0))
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	storageTreeNode := &StorageTreeNode{}
	err = rlp.DecodeBytes(rlpBytes, storageTreeNode)
	if err != nil {
		return err
	}
	tree.root = storageTreeNode.ToTreeNode(0, tree.nilHashes, tree.hasher)

	tree.rootSize = tree.root.Size()
	for i := 0; i < len(tree.root.Children); i++ {
		if tree.root.Children[i] != nil {
			tree.rootSize += uint64(versionSize * len(tree.root.Children[i].Versions))
		}
	}

	return nil
}

func (tree *BASSparseMerkleTree) extendNode(node *TreeNode, nibble, path uint64, depth uint8, isCreated bool) error {
	if node.Children[nibble] != nil &&
		!node.Children[nibble].IsTemporary() {
		return nil
	}

	rlpBytes, err := tree.db.Get(storageFullTreeNodeKey(depth, path))
	if errors.Is(err, database.ErrDatabaseNotFound) {
		if isCreated {
			node.Children[nibble] = NewTreeNode(depth, path, tree.nilHashes, tree.hasher)
		}
		return nil
	}
	if err != nil {
		return err
	}

	storageTreeNode := &StorageTreeNode{}
	err = rlp.DecodeBytes(rlpBytes, storageTreeNode)
	if err != nil {
		return err
	}
	node.Children[nibble] = storageTreeNode.ToTreeNode(
		depth, tree.nilHashes, tree.hasher)

	return nil
}

func (tree *BASSparseMerkleTree) Size() uint64 {
	return tree.rootSize
}

func (tree *BASSparseMerkleTree) Get(key uint64, version *Version) ([]byte, error) {
	if tree.IsEmpty() {
		return nil, ErrEmptyRoot
	}

	if key >= 1<<tree.maxDepth {
		return nil, ErrInvalidKey
	}

	if version == nil {
		version = &tree.version
	}

	if tree.recentVersion > *version {
		return nil, ErrVersionTooOld
	}

	if *version > tree.version {
		return nil, ErrVersionTooHigh
	}

	// read from cache
	cached, ok := tree.dbCache.Get(key)
	if ok {
		node := cached.(*TreeNode)
		for i := len(node.Versions) - 1; i >= 0; i-- {
			if node.Versions[i].Ver <= *version {
				return node.Versions[i].Hash, nil
			}
		}
	}

	// read from db if cache miss
	rlpBytes, err := tree.db.Get(storageFullTreeNodeKey(tree.maxDepth, key))
	if errors.Is(err, database.ErrDatabaseNotFound) {
		return nil, ErrNodeNotFound
	}
	if err != nil {
		return nil, err
	}
	storageTreeNode := &StorageTreeNode{}
	err = rlp.DecodeBytes(rlpBytes, storageTreeNode)
	if err != nil {
		return nil, err
	}

	// cache node that read from db
	tree.dbCache.Add(key, storageTreeNode.ToTreeNode(tree.maxDepth, tree.nilHashes, tree.hasher))

	for i := len(storageTreeNode.Versions) - 1; i >= 0; i-- {
		if storageTreeNode.Versions[i].Ver <= *version {
			return storageTreeNode.Versions[i].Hash, nil
		}
	}

	return tree.nilHashes.Get(tree.maxDepth), nil
}

func (tree *BASSparseMerkleTree) Set(key uint64, val []byte) error {
	if key >= 1<<tree.maxDepth {
		return ErrInvalidKey
	}
	newVersion := tree.version + 1

	targetNode := tree.root
	var depth uint8 = 4
	var parentNodes = make([]*TreeNode, 0, tree.maxDepth/4)
	for i := 0; i < int(tree.maxDepth)/4; i++ {
		// path <= 2^maxDepth - 1
		path := key >> (int(tree.maxDepth) - (i+1)*4)
		// position in treeNode, nibble <= 0xf
		nibble := path & 0x000000000000000f
		parentNodes = append(parentNodes, targetNode.Copy())
		if err := tree.extendNode(targetNode, nibble, path, depth, true); err != nil {
			return err
		}
		targetNode = targetNode.Children[nibble]

		depth += 4
	}
	targetNode = targetNode.Copy()
	targetNode.Set(val, newVersion) // update hash of leaf node
	tree.journal.set(journalKey{targetNode.depth, targetNode.path}, targetNode)
	// recompute root hash of middle nodes
	for i := len(parentNodes) - 1; i >= 0; i-- {
		childNibble := key >> (int(tree.maxDepth) - (i+1)*4) & 0x000000000000000f
		parentNodes[i].SetChildren(targetNode, int(childNibble), newVersion)

		targetNode = parentNodes[i]
		tree.journal.set(journalKey{targetNode.depth, targetNode.path}, targetNode)
	}
	tree.root = targetNode
	return nil
}

func (tree *BASSparseMerkleTree) MultiSet(items []Item) error {
	if len(items) == 0 {
		return nil
	}
	newVersion := tree.version + 1
	targetNode := tree.root
	tmpJournal := newJournal()
	wg := sync.WaitGroup{}
	for _, item := range items {
		if item.Key >= 1<<tree.maxDepth {
			return ErrInvalidKey
		}
		var (
			key         = item.Key
			val         = item.Val
			depth uint8 = 4
		)

		// find middle nodes
		for i := 0; i < int(tree.maxDepth)/4; i++ {
			// path <= 2^maxDepth - 1
			path := key >> (int(tree.maxDepth) - (i+1)*4)
			// position in treeNode, nibble <= 0xf
			nibble := path & 0x000000000000000f

			// skip existed node
			if _, exist := tmpJournal.get(journalKey{targetNode.depth, targetNode.path}); !exist {
				tmpJournal.set(journalKey{targetNode.depth, targetNode.path}, targetNode.Copy())
			}

			// create a new treeNode in targetNode
			if err := tree.extendNode(targetNode, nibble, path, depth, true); err != nil {
				return err
			}
			targetNode = targetNode.Children[nibble]
			depth += 4
		}
		targetNode = targetNode.Copy()
		targetNode.Set(val, newVersion) // update hash of leaf node
		tmpJournal.set(journalKey{targetNode.depth, targetNode.path}, targetNode)

		// recompute root hash of middle nodes in parallel
		// TODO: Improved parallel computation for each depth, avoiding double computation of hashes
		wg.Add(1)
		func(child *TreeNode) {
			tree.goroutinePool.Submit(func() {
				defer wg.Done()
				for child != nil {
					parentKey := journalKey{depth: child.depth - 4, path: child.path >> 4}
					parent, exist := tmpJournal.get(parentKey)
					if !exist {
						// skip if the parent is not exist
						return
					}

					// update child to parent node
					parent.SetChildren(child, int(child.path&0x000000000000000f), newVersion)
					child = parent
				}
			})
		}(targetNode)
	}
	wg.Wait()

	// point root node to the new one
	newRoot, exist := tmpJournal.get(journalKey{tree.root.depth, tree.root.path})
	if !exist {
		return ErrUnexpected
	}
	tree.root = newRoot

	// flush into journal
	tmpJournal.iterate(func(key journalKey, val *TreeNode) error {
		tree.journal.set(key, val)
		return nil
	})

	return nil
}

func (tree *BASSparseMerkleTree) MultiUpdate(items []Item) error {
	if len(items) == 0 {
		return nil
	}
	// also check len(items) not exceed 2^maxDepth - 1
	// also check no duplicated keys
	tmpJournal := newJournal()
	leavesJournal := newJournal()
	//wg := sync.WaitGroup{}
	// should we initialize all intermediate nodes when New SMT? so we can skip this step
	// 1. generate all intermediate nodes, with lock
	// 2. set all leaves, without lock
	// 3. re-compute hash, care about dependency routes
	wg := sync.WaitGroup{}
	wg.Add(len(items))
	maxKey := uint64(1 << tree.maxDepth)
	for _, it := range items {
		i := it
		tree.goroutinePool.Submit(func() {
			defer wg.Done()
			if i.Key >= maxKey {
				panic(ErrInvalidKey)
			}
			//tmp, err := tree.setIntermediateAndLeaves(i)
			leaf, err := tree.setIntermediateAndLeaves(tmpJournal, i)
			if _, exist := leavesJournal.get(journalKey{leaf.depth, leaf.path}); !exist {
				leavesJournal.set(journalKey{leaf.depth, leaf.path}, leaf)
			}
			if err != nil {
				panic(err)
				//fmt.Println(tmp.data)
			}
		})
	}

	wg.Wait()
	//_ = tmpJournal.iterate(func(k journalKey, v *TreeNode) error {
	//	fmt.Printf("Target: %d - %d, %p\n", k.depth, k.path, v)
	//	for _, c := range v.Children {
	//		if c != nil {
	//			fmt.Printf("  child: %d - %d, %p\n", c.depth, c.path, c)
	//		}
	//	}
	//	return nil
	//})

	//3. re-compute hash, care about dependency routes
	leavesSize := leavesJournal.len()
	wg.Add(leavesSize)
	//tree.goroutinePool.Submit(func() {
	//	for {
	//		select {
	//		case jk := <-ch:
	//			log.Printf("received jk: %v\n", jk)
	//			node, exist := tmpJournal.get(jk)
	//			if exist {
	//				tree.goroutinePool.Submit(func() {
	//					defer wg.Done()
	//					tree.recompute(jk, node)
	//				})
	//			}
	//		}
	//	}
	//})
	// 对于treeNode来说，并发度只需设置为叶子节点
	leavesJournal.iterate(func(k journalKey, v *TreeNode) error {
		// 对每一个treeNode， 如果它的root hash算完了，会得到通知，然后继续往上算， 遇到依赖的hash还未算完，结束，自然会有下一个goroutine来算
		tree.goroutinePool.Submit(func() {
			defer wg.Done()
			v.recompute(tree.goroutinePool, tmpJournal)
		})
		return nil
	})
	wg.Wait()

	// point root node to the new one
	newRoot, exist := tmpJournal.get(journalKey{tree.root.depth, tree.root.path})
	if !exist {
		return ErrUnexpected
	}
	//fmt.Printf("Recomputed hash: %x\n", newRoot.Root())
	tree.root = newRoot

	// flush into journal
	tmpJournal.iterate(func(key journalKey, val *TreeNode) error {
		tree.journal.set(key, val)
		return nil
	})

	return nil
}

// return leaf node
func (tree *BASSparseMerkleTree) setIntermediateAndLeaves(tmpJournal *journal, item Item) (*TreeNode, error) {
	var (
		key         = item.Key
		val         = item.Val
		depth uint8 = 4
	)
	newVersion := tree.version + 1
	targetNode := tree.root
	// find middle nodes
	for i := 0; i < int(tree.maxDepth)/4; i++ {
		// path <= 2^maxDepth - 1
		path := key >> (int(tree.maxDepth) - (i+1)*4)
		// position in treeNode, nibble <= 0xf
		nibble := path & 0x000000000000000f

		// skip existed node
		if _, exist := tmpJournal.get(journalKey{targetNode.depth, targetNode.path}); !exist {
			tmpJournal.set(journalKey{targetNode.depth, targetNode.path}, targetNode.Copy())
		}

		// create a new treeNode in targetNode
		if err := tree.extendNode(targetNode, nibble, path, depth, true); err != nil {
			return nil, err
		}
		targetNode = targetNode.Children[nibble]
		depth += 4
	}
	targetNode = targetNode.Copy()
	targetNode.Set(val, newVersion) // update hash of leaf node
	tmpJournal.set(journalKey{targetNode.depth, targetNode.path}, targetNode)
	return targetNode, nil
}

func (tree *BASSparseMerkleTree) IsEmpty() bool {
	return bytes.Equal(tree.root.Root(), tree.nilHashes.Get(0))
}

func (tree *BASSparseMerkleTree) Root() []byte {
	return tree.root.Root()
}

func (tree *BASSparseMerkleTree) GetProof(key uint64) (Proof, error) {
	proofs := make([][]byte, 0, tree.maxDepth/4)
	if tree.IsEmpty() {
		for i := tree.maxDepth; i > 0; i-- {
			proofs = append(proofs, tree.nilHashes.Get(i))
		}
		return proofs, nil
	}

	if key >= 1<<tree.maxDepth {
		return nil, ErrInvalidKey
	}

	targetNode := tree.root
	var neighborNode *TreeNode
	var depth uint8 = 4

	for i := 0; i < int(tree.maxDepth)/4; i++ {
		path := key >> (int(tree.maxDepth) - (i+1)*4)
		nibble := path & 0x000000000000000f
		if err := tree.extendNode(targetNode, nibble, path, depth, true); err != nil {
			return nil, err
		}
		index := 0
		for j := 0; j < 3; j++ {
			// nibble / 8
			// nibble / 4
			// nibble / 2
			inc := int(nibble) / (1 << (3 - j))
			proofs = append(proofs, targetNode.Internals[(index+inc)^1])
			index += 1 << (j + 1)
		}

		neighborNode = targetNode.Children[nibble^1]
		targetNode = targetNode.Children[nibble]
		if neighborNode == nil {
			proofs = append(proofs, tree.nilHashes.Get(depth))
		} else {
			proofs = append(proofs, neighborNode.Root())
		}

		depth += 4
	}

	return utils.ReverseBytes(proofs[:]), nil
}

func (tree *BASSparseMerkleTree) VerifyProof(key uint64, proof Proof) bool {
	if key >= 1<<tree.maxDepth {
		return false
	}

	keyVal, err := tree.Get(key, nil)
	if err != nil && !errors.Is(err, ErrNodeNotFound) && !errors.Is(err, ErrEmptyRoot) {
		return false
	}
	if len(keyVal) == 0 {
		keyVal = tree.nilHashes.Get(tree.maxDepth)
	}

	var depth uint8 = 4
	var helpers = make([]int, 0, tree.maxDepth/4)

	for i := 0; i < int(tree.maxDepth)/4; i++ {
		path := key >> (int(tree.maxDepth) - (i+1)*4)
		nibble := path & 0x000000000000000f

		if i > 0 { // ignore the root node
			helpers = append(helpers, int(path)/16%2)
		}

		index := 0
		for j := 0; j < 3; j++ {
			// nibble / 8
			// nibble / 4
			// nibble / 2
			inc := int(nibble) / (1 << (3 - j))
			helpers = append(helpers, inc%2)
			index += 1 << (j + 1)
		}

		depth += 4
	}
	helpers = append(helpers, int(key)%2)
	helpers = utils.ReverseInts(helpers)
	if len(proof) != len(helpers) {
		return false
	}

	root := tree.Root()
	node := keyVal
	for i := 0; i < len(proof); i++ {
		switch helpers[i] {
		case 0:
			node = tree.hasher.Hash(node, proof[i])
		case 1:
			node = tree.hasher.Hash(proof[i], node)
		default:
			return false
		}
	}

	return bytes.Equal(root, node)
}

func (tree *BASSparseMerkleTree) LatestVersion() Version {
	return tree.version
}

func (tree *BASSparseMerkleTree) RecentVersion() Version {
	return tree.recentVersion
}

func (tree *BASSparseMerkleTree) Reset() {
	tree.journal.flush()
	tree.root = tree.lastSaveRoot
	tree.rootSize = tree.lastSaveRootSize
}

func (tree *BASSparseMerkleTree) writeNode(db database.Batcher, fullNode *TreeNode, version Version, recentVersion *Version) (uint64, error) {
	changed := uint64(0)
	if fullNode.PreviousVersion() > tree.gcStatus.latestGCVersion {
		// If the previous version is greater than the last GC version,
		// the node has a high probability of existing in memory
		changed = versionSize
	} else {
		changed = fullNode.Size()
	}
	// prune versions
	if recentVersion != nil {
		changed -= fullNode.Prune(*recentVersion)
	}

	// persist tree
	rlpBytes, err := rlp.EncodeToBytes(fullNode.ToStorageTreeNode())
	if err != nil {
		return changed, err
	}
	err = db.Set(storageFullTreeNodeKey(fullNode.depth, fullNode.path), rlpBytes)
	if err != nil {
		return changed, err
	}
	if db.ValueSize() > tree.batchSizeLimit {
		if err := db.Write(); err != nil {
			return changed, err
		}
		db.Reset()
	}

	return changed, nil
}

func (tree *BASSparseMerkleTree) Commit(recentVersion *Version) (Version, error) {
	newVersion := tree.version + 1
	if recentVersion != nil && *recentVersion >= newVersion {
		return tree.version, ErrVersionTooHigh
	}

	size := uint64(0)
	journalSize := tree.journal.len()
	if tree.db != nil {
		// write tree nodes, prune old version
		batch := tree.db.NewBatch()
		err := tree.journal.iterate(func(key journalKey, node *TreeNode) error {
			changed, err := tree.writeNode(batch, node, newVersion, recentVersion)
			if err != nil {
				return err
			}
			size += changed
			if node.depth == tree.maxDepth { // leaf node
				tree.dbCache.Add(node.path, node)
			}
			return nil
		})
		if err != nil {
			return tree.version, err
		}
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(newVersion))
		err = batch.Set(latestVersionKey, buf)
		if err != nil {
			return tree.version, err
		}

		if recentVersion != nil {
			buf = make([]byte, 8)
			binary.BigEndian.PutUint64(buf, uint64(*recentVersion))
			err = batch.Set(recentVersionNumberKey, buf)
			if err != nil {
				return tree.version, err
			}
		}

		err = batch.Write()
		if err != nil {
			return tree.version, err
		}
		batch.Reset()
	}

	tree.version = newVersion
	if recentVersion != nil {
		tree.recentVersion = *recentVersion
	}
	originSize := tree.rootSize
	currentSize := tree.rootSize + size
	if releaseVersion := tree.gcStatus.pop(currentSize); releaseVersion > 0 {
		currentSize = tree.root.Release(releaseVersion)
	}
	tree.gcStatus.add(tree.version, currentSize)
	tree.journal.flush()
	tree.lastSaveRoot = tree.root
	tree.lastSaveRootSize = originSize
	tree.rootSize = currentSize

	if tree.metrics != nil {
		tree.metrics.CommitNum(journalSize)
		tree.metrics.ChangeSize(size)
		tree.metrics.CurrentSize(currentSize)
		tree.metrics.Version(uint64(tree.version))
		tree.metrics.PrunedVersion(uint64(tree.recentVersion))
		tree.collectGCMetrics()
	}

	return newVersion, nil
}

func (tree *BASSparseMerkleTree) rollback(child *TreeNode, oldVersion Version, db database.Batcher) (uint64, error) {
	// remove value nodes
	next, changed := child.Rollback(oldVersion)
	if !next {
		return changed, nil
	}

	// re-cache the rollback node
	if child.depth == tree.maxDepth && tree.dbCache.Contains(child.path) {
		tree.dbCache.Add(child.path, child)
	}

	for nibble, subChild := range child.Children {
		if subChild != nil {
			subDepth := child.depth + 4
			err := tree.extendNode(child, uint64(nibble), subChild.path, subDepth, false)
			if err != nil {
				return changed, err
			}

			subChanged, err := tree.rollback(child.Children[nibble], oldVersion, db)
			if err != nil {
				return changed, err
			}
			changed += subChanged
		}
		child.ComputeInternalHash()
	}

	// persist tree
	rlpBytes, err := rlp.EncodeToBytes(child.ToStorageTreeNode())
	if err != nil {
		return changed, err
	}
	err = db.Set(storageFullTreeNodeKey(child.depth, child.path), rlpBytes)
	if err != nil {
		return changed, err
	}
	if db.ValueSize() > tree.batchSizeLimit {
		if err := db.Write(); err != nil {
			return changed, err
		}
		db.Reset()
	}

	return changed, nil
}

func (tree *BASSparseMerkleTree) Rollback(version Version) error {
	if tree.IsEmpty() {
		return ErrEmptyRoot
	}

	if tree.recentVersion > version {
		return ErrVersionTooOld
	}

	if version > tree.version {
		return ErrVersionTooHigh
	}

	tree.Reset()

	newVersion := version
	originSize := tree.rootSize
	size := tree.rootSize
	if tree.db != nil {
		batch := tree.db.NewBatch()
		changed, err := tree.rollback(tree.root, version, batch)
		if err != nil {
			return err
		}
		size -= changed
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(newVersion))
		err = batch.Set(latestVersionKey, buf)
		if err != nil {
			return err
		}

		err = batch.Write()
		if err != nil {
			return err
		}
		batch.Reset()
	}

	tree.version = newVersion
	tree.rootSize = size

	if tree.metrics != nil {
		tree.metrics.ChangeSize(originSize - size)
		tree.metrics.CurrentSize(size)
		tree.metrics.Version(uint64(tree.version))
		tree.metrics.PrunedVersion(uint64(tree.recentVersion))
	}
	return nil
}

func (tree *BASSparseMerkleTree) collectGCMetrics() {
	tree.metrics.LatestGCVersion(uint64(tree.gcStatus.latestGCVersion))
	var gcVersions [10]*metrics.GCVersion
	for i := range tree.gcStatus.versions {
		gcVersions[i] = &metrics.GCVersion{
			Version: uint64(tree.gcStatus.versions[i]),
			Size:    uint64(tree.gcStatus.sizes[i]),
		}
	}
	tree.metrics.GCVersions(gcVersions)
}

func (tree *BASSparseMerkleTree) recompute(k journalKey, v *TreeNode) {

}

func hashesToCompute(nibble uint64) (keys []uint64) {
	index := 0
	for j := 0; j < 3; j++ {
		inc := int(nibble) / (1 << (3 - j))
		keys = append(keys, uint64(index+inc))
		index += 1 << (j + 1)
	}
	return
}

type hashCoordinate struct {
	depth  uint8
	nibble uint64
}

type nodeWithNibble struct {
	*journal
	nibbles map[journalKey]map[uint64]struct{}
}

func newNodeWithNibble(j *journal) *nodeWithNibble {
	return &nodeWithNibble{
		journal: j,
		nibbles: make(map[journalKey]map[uint64]struct{}),
	}
}

func (n *nodeWithNibble) getNibbles(k journalKey) map[uint64]struct{} {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.nibbles[k]
}

func (n *nodeWithNibble) setNibbles(k journalKey, nibbles []uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.nibbles[k] == nil {
		n.nibbles[k] = make(map[uint64]struct{})
	}
	for _, ni := range nibbles {
		n.nibbles[k][ni] = struct{}{}
	}
}

func (n *nodeWithNibble) iterateNibbles(callback func(k journalKey, v map[uint64]struct{}) error) error {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for key, val := range n.nibbles {
		err := callback(key, val)
		if err != nil {
			return err
		}
	}
	return nil
}
