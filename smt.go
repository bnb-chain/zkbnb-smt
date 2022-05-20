package bsmt

import (
	"bytes"
	"encoding/binary"
	"hash"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/bnb-chain/bas-smt/database"
	"github.com/bnb-chain/bas-smt/database/memory"
	"github.com/bnb-chain/bas-smt/utils"
)

var (
	emptyHash                 = []byte(nil)
	latestVersionKey          = []byte(`latestVersion`)
	recentVersionNumberKey    = []byte(`recentVersionNumber`)
	storageValueNodePrefix    = []byte(`v`)
	storaegFullTreeNodePrefix = []byte(`t`)
	sep                       = []byte(`:`)
)

func storageValueNodeKey(depth uint64, path []byte, version Version) []byte {
	versionBuf := make([]byte, 8)
	depthBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(versionBuf, uint64(version))
	binary.BigEndian.PutUint64(depthBuf, depth)

	return bytes.Join([][]byte{storageValueNodePrefix, versionBuf, depthBuf, path}, sep)
}

func storageFullTreeNodeKey(depth uint64, path []byte) []byte {
	depthBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(depthBuf, depth)
	return bytes.Join([][]byte{storaegFullTreeNodePrefix, depthBuf, path}, sep)
}

var _ SparseMerkleTree = (*BASSparseMerkleTree)(nil)

func NewBASSparseMerkleTree(hasher hash.Hash, db database.TreeDB, maxVersionNum, maxDepth uint64,
	opts ...Option) (SparseMerkleTree, error) {
	smt := &BASSparseMerkleTree{
		root:          NewFullNode(hasher, nil, nil, nil, 0, nil),
		maxDepth:      maxDepth,
		maxVersionNum: maxVersionNum,
		hasher:        hasher,
	}

	if db == nil {
		db = memory.NewMemoryDB()
	}

	root, _ := recoveryTree(smt, 1, db)
	smt.root = root
	smt.db = db

	for _, opt := range opts {
		opt(smt)
	}
	return smt, nil
}

func recoveryTree(smt *BASSparseMerkleTree, layers uint64, db database.TreeDB) (TreeNode, error) {
	root := NewFullNode(smt.hasher, nil, nil, nil, 0, db)

	buf, err := db.Get(latestVersionKey)
	if err != nil {
		return root, err
	}
	smt.version = Version(binary.BigEndian.Uint64(buf))
	buf, err = db.Get(recentVersionNumberKey)
	if err != nil {
		return root, err
	}
	smt.recentVersion = Version(binary.BigEndian.Uint64(buf))

	// left
	key := []byte{0}
	buf, err = db.Get(storageFullTreeNodeKey(1, key))
	if err == nil {
		storageFullNode := &StorageFullTreeNode{}
		err = rlp.DecodeBytes(buf, storageFullNode)
		if err != nil {
			return root, err
		}
		root.leftChild = storageFullNode.ToFullTreeNode(smt.hasher, db, key)
	}

	// right
	key = []byte{1}
	buf, err = db.Get(storageFullTreeNodeKey(1, key))
	if err == nil {
		storageFullNode := &StorageFullTreeNode{}
		err = rlp.DecodeBytes(buf, storageFullNode)
		if err != nil {
			return root, err
		}
		root.rightChild = storageFullNode.ToFullTreeNode(smt.hasher, db, key)
	}

	return root, nil
}

type BASSparseMerkleTree struct {
	version       Version
	recentVersion Version
	root          TreeNode // The working root node
	lastSavedRoot TreeNode // The most recently saved root node
	maxDepth      uint64
	maxVersionNum uint64

	hasher hash.Hash
	db     database.TreeDB
}

func (tree *BASSparseMerkleTree) Get(key []byte, version *Version) ([]byte, error) {
	if tree.root == nil {
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

	node, err := tree.root.Get(key)
	if err != nil {
		return nil, err
	}

	for i, v := range node.Versions() {
		if v <= *version {
			if i == 0 {
				return node.Root(), nil
			}
			return tree.db.Get(storageValueNodeKey(node.Depth(), key, *version))
		}
	}
	return nil, ErrNodeNotFound
}

func (tree *BASSparseMerkleTree) Set(key, val []byte) error {
	if tree.root == nil {
		return ErrEmptyRoot
	}
	newRoot, err := tree.root.Set(key, val, tree.version+1)
	if err != nil {
		return err
	}
	tree.root = newRoot
	return nil
}

func (tree *BASSparseMerkleTree) IsEmpty(key []byte, version *Version) bool {
	val, _ := tree.Get(key, version)
	return val != nil
}

func (tree *BASSparseMerkleTree) Root() []byte {
	return tree.root.Root()
}

func (tree *BASSparseMerkleTree) GetProof(key []byte, version *Version) (*Proof, error) {
	if tree.root == nil {
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

	proofs, helpers, err := tree.root.GetProof(key, *version)
	if err != nil {
		return nil, err
	}
	if len(helpers) > 0 {
		helpers = helpers[1:] // ignore root
	}

	return &Proof{
		MerkleProof: proofs,
		ProofHelper: helpers,
	}, nil
}

func (tree *BASSparseMerkleTree) VerifyProof(proof *Proof, version *Version) bool {
	if tree.root == nil {
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
	return tree.root.VerifyProof(utils.ReverseBytes(proof.MerkleProof), utils.ReverseInts(proof.ProofHelper), *version)
}

func (tree *BASSparseMerkleTree) LatestVersion() Version {
	return tree.version
}

func (tree *BASSparseMerkleTree) Reset() {
	tree.root = tree.lastSavedRoot
}

func (tree *BASSparseMerkleTree) writeNode(db database.Batcher, node TreeNode, version Version, isPersist map[string]struct{}) error {
	fullNode, ok := node.(*FullTreeNode)
	if !ok {
		return nil
	}

	// write value node
	err := db.Set(storageValueNodeKey(fullNode.depth, fullNode.key, version), fullNode.latestHash)
	if err != nil {
		return err
	}

	// prune
	if len(fullNode.versions) > int(tree.maxVersionNum) {
		prune := fullNode.versions[tree.maxVersionNum:]
		for _, version := range prune {
			err := db.Delete(storageValueNodeKey(fullNode.depth, fullNode.key, version))
			if err != nil {
				return err
			}
		}
		fullNode.versions = fullNode.versions[:tree.maxVersionNum]
	}

	// persist tree
	if fullNode.depth%4 != 1 {
		return nil
	}
	if _, ok := isPersist[string(fullNode.key)]; ok {
		// skip
		return nil
	}
	storageNode := fullNode.ToStorageFullNode()
	rlpBytes, err := rlp.EncodeToBytes(storageNode)
	if err != nil {
		return err
	}
	err = db.Set(storageFullTreeNodeKey(fullNode.depth, fullNode.key), rlpBytes)
	if err != nil {
		return err
	}
	isPersist[string(fullNode.key)] = struct{}{}
	fullNode.dirty = false
	return nil
}

func (tree *BASSparseMerkleTree) retrieveDirtyNode(root TreeNode, collecion map[string]TreeNode) {
	if !root.Dirty() {
		return
	}
	collecion[string(root.Key())] = root
	if root.Left() != nil {
		tree.retrieveDirtyNode(root.Left(), collecion)
	}
	if root.Right() != nil {
		tree.retrieveDirtyNode(root.Right(), collecion)
	}
}

func (tree *BASSparseMerkleTree) Commit() (Version, error) {
	newVersion := tree.version + 1

	if tree.db != nil {
		// write tree nodes, prune old version
		batch := tree.db.NewBatch()
		isPersist := map[string]struct{}{}
		collection := map[string]TreeNode{}
		tree.retrieveDirtyNode(tree.root, collection)
		for _, node := range collection {
			err := tree.writeNode(batch, node, newVersion, isPersist)
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

	tree.version = newVersion
	tree.lastSavedRoot = tree.root
	return newVersion, nil
}

func (tree *BASSparseMerkleTree) rollback(root TreeNode, oldVersion Version, db database.Batcher) error {
	versions := root.Versions()
	if len(versions) > 0 {
		var (
			i       int
			version Version
		)
		for i, version = range versions {
			if version <= oldVersion {
				break
			}
			err := db.Delete(storageValueNodeKey(root.Depth(), root.Key(), version))
			if err != nil {
				return err
			}
		}
		// set old hash
		if i > len(versions) {
			version = versions[i]
			hash, err := tree.db.Get(storageValueNodeKey(root.Depth(), root.Key(), version))
			if err != nil {
				return err
			}
			root.SetRoot(hash)
			root.SetVersions(versions[i:])
		} else {
			root.SetRoot(emptyHash)
			root.SetVersions(nil)
		}
	}

	if root.Left() != nil {
		err := tree.rollback(root.Left(), oldVersion, db)
		if err != nil {
			return err
		}
	}
	if root.Right() != nil {
		err := tree.rollback(root.Right(), oldVersion, db)
		if err != nil {
			return err
		}
	}
	return nil
}

func (tree *BASSparseMerkleTree) Rollback(version Version) error {
	if tree.root == nil {
		return ErrEmptyRoot
	}

	if tree.recentVersion > version {
		return ErrVersionTooOld
	}

	if version > tree.version {
		return ErrVersionTooHigh
	}

	newVersion := version
	newRecentVersion := uint64(0)
	if uint64(version) > tree.maxVersionNum {
		newRecentVersion = uint64(version) - tree.maxVersionNum
	}
	if tree.db != nil {
		batch := tree.db.NewBatch()
		tree.rollback(tree.root, version, batch)

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
