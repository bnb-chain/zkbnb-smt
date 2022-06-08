package bsmt

import (
	"bytes"
	"encoding/binary"
	"hash"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/bnb-chain/bas-smt/database"
	"github.com/bnb-chain/bas-smt/database/memory"
)

var (
	latestVersionKey          = []byte(`latestVersion`)
	recentVersionNumberKey    = []byte(`recentVersionNumber`)
	storageValueNodePrefix    = []byte(`v`)
	storaegFullTreeNodePrefix = []byte(`t`)
	sep                       = []byte(`:`)
)

func storageValueNodeKey(depth uint8, path uint64, version Version) []byte {
	versionBuf := make([]byte, 8)
	depthBuf := make([]byte, 8)
	pathBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(versionBuf, uint64(version))
	binary.BigEndian.PutUint64(depthBuf, uint64(depth))
	binary.BigEndian.PutUint64(pathBuf, path)
	return bytes.Join([][]byte{storageValueNodePrefix, versionBuf, depthBuf, pathBuf}, sep)
}

func storageFullTreeNodeKey(depth uint8, path uint64) []byte {
	depthBuf := make([]byte, 8)
	pathBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(depthBuf, uint64(depth))
	binary.BigEndian.PutUint64(pathBuf, path)
	return bytes.Join([][]byte{storaegFullTreeNodePrefix, depthBuf, pathBuf}, sep)
}

var _ SparseMerkleTree = (*BASSparseMerkleTree)(nil)

func NewBASSparseMerkleTree(hasher hash.Hash, db database.TreeDB, maxVersionNum, maxDepth uint64, nilHash []byte,
	opts ...Option) (SparseMerkleTree, error) {
	smt := &BASSparseMerkleTree{
		journal:       map[int]*TreeNode{},
		maxDepth:      maxDepth,
		maxVersionNum: maxVersionNum,
		nilHashes:     constuctNilHashes(maxDepth, nilHash, hasher),
		hasher:        hasher,
	}

	for _, opt := range opts {
		opt(smt)
	}

	if db == nil {
		db = memory.NewMemoryDB()
	}

	recoveryTree(smt, 1, db)
	smt.db = db
	if len(smt.child) == 0 {
		smt.child = append(smt.child, NewTreeNode(0, 0, smt.nilHashes, hasher))
	}
	return smt, nil
}

func constuctNilHashes(maxDepth uint64, nilHash []byte, hasher hash.Hash) [][]byte {
	if maxDepth == 0 {
		return [][]byte{nilHash}
	}
	nilHashes := make([][]byte, maxDepth)
	nilHashes[0] = nilHash
	for i := 0; i < int(maxDepth); i++ {
		hasher.Write(nilHash)
		hasher.Write(nilHash)
		nilHash = hasher.Sum(nil)
		hasher.Reset()
		nilHashes[i] = nilHash
	}

	return nilHashes
}

func recoveryTree(smt *BASSparseMerkleTree, layers uint64, db database.TreeDB) {
	// recovery version info
	buf, err := db.Get(latestVersionKey)
	if err != nil {
		return
	}
	smt.version = Version(binary.BigEndian.Uint64(buf))
	buf, err = db.Get(recentVersionNumberKey)
	if err != nil {
		return
	}
	smt.recentVersion = Version(binary.BigEndian.Uint64(buf))

	// recovery tree nodes
	childNum := 1 << layers * 4
	smt.child = make([]*TreeNode, childNum)
	depth := uint8(0)
	elementNum := 1 << depth
	pathItr := uint64(0)
	for i := 0; i < childNum; i++ {
		if i == elementNum {
			pathItr = 0
			depth += 4
			elementNum += 1 << depth
		}
		rlpBytes, _ := db.Get(storageFullTreeNodeKey(depth, pathItr))
		treeNode := &TreeNode{
			depth:  depth,
			path:   pathItr,
			hasher: smt.hasher,
		}
		if rlpBytes != nil {
			rlp.DecodeBytes(rlpBytes, treeNode)
			treeNode.Rebuild()
			smt.child[i] = treeNode
		}

		pathItr++
	}

}

type BASSparseMerkleTree struct {
	version       Version
	recentVersion Version
	child         []*TreeNode       // The working root node
	journal       map[int]*TreeNode // Change nodes
	maxDepth      uint64
	maxVersionNum uint64
	nilHashes     [][]byte
	hasher        hash.Hash
	db            database.TreeDB
}

func (tree *BASSparseMerkleTree) index(key uint64) (uint8, uint64, int) {
	if key == 0 {
		return 0, 0, 0
	}
	elementNum := 1
	start := 0
	index := 0
	depth := 1
	for uint64(elementNum) < key {
		depth++
		if depth > 0 && depth%4 == 0 {
			start = index + 1
			index = index + 2<<depth
		}
		v := 1 << depth
		elementNum += v
	}

	path := uint64(0)
	if depth > 0 {
		path = key - 1<<depth + 1
	}

	if start > 0 {
		index = start + int(path)
	} else {
		index = start
	}
	return uint8(depth), path, index
}

func (tree *BASSparseMerkleTree) getRootNode(depth uint8, path uint64, index int, writable bool) *TreeNode {
	root, ok := tree.journal[index]
	if ok {
		return root
	}

	if index > len(tree.child)-1 {
		for i := len(tree.child) - 1; i < index; i++ {
			tree.child = append(tree.child, nil)
		}
	}

	if tree.child[index] == nil {
		rlpBytes, _ := tree.db.Get(storageFullTreeNodeKey(depth, path))
		if rlpBytes != nil {
			treeNode := &TreeNode{
				depth:  depth,
				path:   path,
				hasher: tree.hasher,
			}
			rlp.DecodeBytes(rlpBytes, treeNode)
			treeNode.Rebuild()
			tree.child[index] = treeNode
		} else if writable {
			tree.child[index] = NewTreeNode(depth, path, tree.nilHashes, tree.hasher)
		}
	}

	return tree.child[index]
}

func (tree *BASSparseMerkleTree) Get(key uint64, version *Version) ([]byte, error) {
	if len(tree.child) == 0 {
		return nil, ErrEmptyRoot
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

	_, hash, err := tree.getChildNode(key, version)
	if err != nil {
		return nil, err
	}

	return hash, nil
}

func (tree *BASSparseMerkleTree) getChildNode(key uint64, version *Version) (*LeafNode, []byte, error) {
	depth, path, index := tree.index(key)
	root := tree.getRootNode(depth, path, index, false)
	if root == nil {
		return nil, nil, ErrNodeNotFound
	}
	node, err := root.Get(key)
	if err != nil {
		return nil, nil, err
	}

	for i, v := range node.Versions {
		if v.Ver <= *version {
			if i == 0 {
				return node, node.Root(), nil
			}
			hash, err := tree.db.Get(storageValueNodeKey(node.depth, node.path, *version))
			if err != nil {
				return nil, nil, err
			}
			return node, hash, nil
		}
	}

	return nil, nil, ErrNodeNotFound
}

func (tree *BASSparseMerkleTree) Set(key uint64, val []byte) {
	newVersion := tree.version + 1
	depth, path, index := tree.index(key)
	root := tree.getRootNode(depth, path, index, true)
	tree.journal[index] = root.Set(key, val, newVersion)

	parentRoot := root
	// recompute the parent hash
	for parentRoot.depth > 0 {
		parentKey := (parentRoot.Key() - 1) >> 1
		parentDepth, parentPath, parentIndex := tree.index(parentKey)
		parentRoot = tree.getRootNode(parentDepth, parentPath, parentIndex, true)

		inc := uint64(0)
		if parentRoot.depth > 0 {
			inc = 1 << (parentRoot.depth - 1)
		}
		leftKey := uint64(parentKey + inc + parentRoot.path)
		leftDepth, leftPath, leftIndex := tree.index(leftKey)
		left := tree.getRootNode(leftDepth, leftPath, leftIndex, true)
		rightKey := leftKey + 1
		rightDepth, rightPath, rightIndex := tree.index(rightKey)
		right := tree.getRootNode(rightDepth, rightPath, rightIndex, true)

		parentRoot = parentRoot.Set(parentKey, tree.Hash(left.Root(), right.Root()), newVersion)
		tree.journal[parentIndex] = parentRoot
	}
}

func (tree *BASSparseMerkleTree) Hash(left, right []byte) []byte {
	tree.hasher.Write(left)
	tree.hasher.Write(right)
	defer tree.hasher.Reset()
	return tree.hasher.Sum(nil)
}

func (tree *BASSparseMerkleTree) IsEmpty() bool {
	return len(tree.child) == 0
}

func (tree *BASSparseMerkleTree) Root() []byte {
	if len(tree.child) == 0 {
		return tree.nilHashes[0]
	}

	return tree.child[0].Root()
}

func (tree *BASSparseMerkleTree) GetProof(key uint64, version *Version) (*Proof, error) {
	if len(tree.child) == 0 {
		return nil, ErrEmptyRoot
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

	var (
		proofs  [][]byte
		helpers []int
	)
	for key > 0 {
		hash, err := tree.Get(key, version)
		if err != nil {
			return nil, err
		}
		proofs = append([][]byte{hash}, proofs...)
		helper := 0
		if int(key%2) == 0 {
			helper = 1
		}
		helpers = append([]int{helper}, helpers...)
		key = (key - 1) >> 1
	}

	proofs = append([][]byte{tree.Root()}, proofs...)

	return &Proof{
		MerkleProof: proofs,
		ProofHelper: helpers, // ignore root
	}, nil
}

func (tree *BASSparseMerkleTree) VerifyProof(proof *Proof, version *Version) bool {
	if len(tree.child) == 0 {
		return false
	}

	if version == nil {
		version = &tree.version
	}

	if tree.recentVersion > *version {
		return false
	}

	if *version > tree.version {
		return false
	}

	key := uint64(0)
	i := 0
	for len(proof.ProofHelper) > 0 && i < len(proof.ProofHelper) {
		node, hash, err := tree.getChildNode(key, version)
		if err != nil {
			return false
		}

		if !bytes.Equal(hash, proof.MerkleProof[i]) {
			return false
		}

		inc := uint64(1)
		if node.depth > 0 {
			inc = 1 << (node.depth)
		}
		key = uint64(key + inc + node.path)
		if proof.ProofHelper[i] == 1 {
			key++
		}
		i++
	}

	return true
}

func (tree *BASSparseMerkleTree) LatestVersion() Version {
	return tree.version
}

func (tree *BASSparseMerkleTree) Reset() {
	tree.journal = map[int]*TreeNode{}
}

func (tree *BASSparseMerkleTree) writeNode(db database.Batcher, fullNode *TreeNode, version Version) error {
	// write value node
	err := db.Set(storageValueNodeKey(fullNode.depth, fullNode.path, version), fullNode.Root())
	if err != nil {
		return err
	}

	// prune
	err = fullNode.Prune(tree.recentVersion, func(depth uint8, path uint64, ver Version) error {
		return db.Delete(storageValueNodeKey(depth, path, ver))
	})
	if err != nil {
		return err
	}

	// persist tree
	rlpBytes, err := rlp.EncodeToBytes(fullNode)
	if err != nil {
		return err
	}
	err = db.Set(storageFullTreeNodeKey(fullNode.depth, fullNode.path), rlpBytes)
	if err != nil {
		return err
	}
	return nil
}

func (tree *BASSparseMerkleTree) Commit() (Version, error) {
	newVersion := tree.version + 1

	if tree.db != nil {
		// write tree nodes, prune old version
		batch := tree.db.NewBatch()
		for _, node := range tree.journal {
			err := tree.writeNode(batch, node, newVersion)
			if err != nil {
				return tree.version, err
			}
		}
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(newVersion))
		err := batch.Set(latestVersionKey, buf)
		if err != nil {
			return tree.version, err
		}

		if uint64(newVersion) > tree.maxVersionNum {
			tree.recentVersion++
		}
		buf = make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(tree.recentVersion))
		err = batch.Set(recentVersionNumberKey, buf)
		if err != nil {
			return tree.version, err
		}

		err = batch.Write()
		if err != nil {
			return tree.version, err
		}
	}
	for index, root := range tree.journal {
		tree.child[index] = root
	}
	tree.version = newVersion
	tree.journal = map[int]*TreeNode{}
	return newVersion, nil
}

func (tree *BASSparseMerkleTree) rollback(oldVersion Version, db database.Batcher) error {
	for _, child := range tree.child {
		if child == nil {
			continue
		}
		// remove value nodes
		next, err := child.Rollback(oldVersion, func(depth uint8, path uint64, ver Version) error {
			return db.Delete(storageValueNodeKey(depth, path, ver))
		})
		if err != nil {
			return err
		}

		// persist tree
		rlpBytes, err := rlp.EncodeToBytes(child)
		if err != nil {
			return err
		}
		err = db.Set(storageFullTreeNodeKey(child.depth, child.path), rlpBytes)
		if err != nil {
			return err
		}
		if !next {
			break
		}
	}

	return nil
}

func (tree *BASSparseMerkleTree) Rollback(version Version) error {
	if len(tree.child) == 0 {
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
	newRecentVersion := uint64(0)
	if uint64(version) > tree.maxVersionNum {
		newRecentVersion = uint64(version) - tree.maxVersionNum
	}
	if tree.db != nil {
		batch := tree.db.NewBatch()
		tree.rollback(version, batch)

		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(newVersion))
		err := batch.Set(latestVersionKey, buf)
		if err != nil {
			return err
		}

		buf = make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(newRecentVersion))
		err = batch.Set(recentVersionNumberKey, buf)
		if err != nil {
			return err
		}

		err = batch.Write()
		if err != nil {
			return err
		}
	}

	tree.version = newVersion
	tree.recentVersion = Version(newRecentVersion)
	return nil
}
