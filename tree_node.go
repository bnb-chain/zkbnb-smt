package bsmt

import (
	"bytes"
	"hash"

	"github.com/bnb-chain/bas-smt/database"
	"github.com/bnb-chain/bas-smt/utils"
)

const (
	left = iota
	right
)

var (
	_ TreeNode = (*FullTreeNode)(nil)
)

func NewFullNode(hasher hash.Hash, key, hash []byte, versions []Version, depth uint64, db database.TreeDB) *FullTreeNode {
	return &FullTreeNode{
		latestHash: hash,
		versions:   versions,
		key:        key,
		depth:      depth,
		size:       uint64(len(key) + len(hash)),
		hasher:     hasher,
		db:         db,
	}
}

type FullTreeNode struct {
	latestHash []byte
	versions   []Version
	key        []byte
	depth      uint64
	leftChild  TreeNode
	rightChild TreeNode
	size       uint64
	dirty      bool
	hasher     hash.Hash
	db         database.TreeDB
}

func (node *FullTreeNode) Root() []byte {
	return node.latestHash
}

func (node *FullTreeNode) SetRoot(hash []byte) {
	node.latestHash = hash
}

func (node *FullTreeNode) Dirty() bool {
	return node.dirty
}

func (node *FullTreeNode) Depth() uint64 {
	return node.depth
}

func (node *FullTreeNode) Key() []byte {
	return node.key
}

func (node *FullTreeNode) Versions() []Version {
	return node.versions
}

func (node *FullTreeNode) SetVersions(versions []Version) {
	node.versions = versions
}

func (node *FullTreeNode) Left() TreeNode {
	if node.leftChild == nil {
		node.leftChild = node.extend(append(utils.CopyBytes(node.key), left))
	}
	return node.leftChild
}

func (node *FullTreeNode) Right() TreeNode {
	if node.rightChild == nil {
		node.rightChild = node.extend(append(utils.CopyBytes(node.key), right))
	}
	return node.rightChild
}

func (node *FullTreeNode) getChild(symbol byte) TreeNode {
	switch symbol {
	case left:
		return node.Left()
	case right:
		return node.Right()
	}

	return nil
}

func (node *FullTreeNode) Get(key []byte) (TreeNode, error) {
	if bytes.Equal(key, node.key) {
		return node, nil
	}

	if len(key) == int(node.depth) {
		return nil, ErrNodeNotFound
	}

	child := node.getChild(key[node.depth])
	if child == nil {
		return nil, ErrNodeNotFound
	}

	return child.Get(key)
}

func (node *FullTreeNode) GetProof(key []byte, version Version) ([][]byte, []int, error) {
	proof, err := node.db.Get(storageValueNodeKey(node.depth, node.key, version))
	if err != nil {
		return nil, nil, err
	}
	helper := 0
	if len(node.key) > 0 {
		helper = int(node.key[len(node.key)-1])
	}

	if bytes.Equal(key, node.key) {
		return [][]byte{node.latestHash}, []int{helper}, nil
	}

	if len(key) == int(node.depth) {
		return nil, nil, ErrNodeNotFound
	}

	child := node.getChild(key[node.depth])
	if child == nil {
		return nil, nil, ErrNodeNotFound
	}
	proofs, helpers, err := child.GetProof(key, version)
	if err != nil {
		return nil, nil, err
	}
	return append(proofs, proof), append(helpers, int(key[node.depth])), nil
}

func (node *FullTreeNode) VerifyProof(proofs [][]byte, helpers []int, version Version) bool {
	if len(helpers) == 0 {
		return false
	}

	proof, err := node.db.Get(storageValueNodeKey(node.depth, node.key, version))
	if err != nil {
		return false
	}

	if !bytes.Equal(proofs[node.depth], proof) {
		return false
	}

	if len(helpers) == int(node.depth) {
		return true
	}

	child := node.getChild(byte(helpers[node.depth]))
	if child == nil {
		return false
	}
	return child.VerifyProof(proofs, helpers, version)
}

func (node *FullTreeNode) Set(key, val []byte, version Version) (TreeNode, error) {
	n := node.Copy()
	if bytes.Equal(node.key, key) {
		n.latestHash = val
		n.versions = append([]Version{version}, n.versions...)
		n.size = uint64(len(key) + len(val))
		return n, nil
	}

	if len(key) > int(n.depth) {
		if n.Left() == nil {
			leftKey := append(utils.CopyBytes(node.key), left)
			n.leftChild = NewFullNode(node.hasher, leftKey, emptyHash, nil, node.depth+1, node.db)
		}
		if n.Right() == nil {
			rightKey := append(utils.CopyBytes(node.key), right)
			n.rightChild = NewFullNode(node.hasher, rightKey, emptyHash, nil, node.depth+1, node.db)
		}
	}

	defer func() {
		n.latestHash = n.HashSubTree()
		n.versions = append([]Version{version}, n.versions...)
		n.size = uint64(len(key) + len(n.latestHash))
	}()

	switch key[n.depth] {
	case left:
		copied, err := n.leftChild.Set(key, val, version)
		if err != nil {
			return nil, err
		}
		n.leftChild = copied
	case right:
		copied, err := n.rightChild.Set(key, val, version)
		if err != nil {
			return nil, err
		}
		n.rightChild = copied
	}
	return n, nil
}

func (node *FullTreeNode) HashSubTree() []byte {
	node.hasher.Write(node.leftChild.Root())
	node.hasher.Write(node.rightChild.Root())
	defer node.hasher.Reset()
	return node.hasher.Sum(nil)
}

func (node *FullTreeNode) Copy() *FullTreeNode {
	return &FullTreeNode{
		latestHash: node.latestHash,
		versions:   node.versions,
		key:        node.key,
		depth:      node.depth,
		leftChild:  node.leftChild,
		rightChild: node.rightChild,
		size:       node.size,
		db:         node.db,
		hasher:     node.hasher,
		dirty:      true,
	}
}

func (node *FullTreeNode) extend(key []byte) TreeNode {
	length := len(key)
	if node.db != nil && length > 1 && length%4 == 1 { // try to load from database { // try to load from database
		storageFullNode, _ := recoveryStorageFullTreeNode(node.db, key)
		if storageFullNode != nil {
			return storageFullNode.ToFullTreeNode(node.hasher, node.db, key)
		}
	}
	return nil
}

func (node *FullTreeNode) flatten(depth int) []*StorageShortTreeNode {
	if depth == 0 {
		return nil
	}
	shortNodes := make([]*StorageShortTreeNode, 0, 2<<depth-1)
	shortNodes = append(shortNodes, &StorageShortTreeNode{
		Key:        node.key,
		LatestHash: node.latestHash,
		Versions:   node.versions,
	})

	if node.leftChild != nil {
		shortNodes = append(shortNodes, node.leftChild.(*FullTreeNode).flatten(depth-1)...)
	}

	if node.rightChild != nil {
		shortNodes = append(shortNodes, node.rightChild.(*FullTreeNode).flatten(depth-1)...)
	}

	return shortNodes
}

func (node *FullTreeNode) ToStorageFullNode() *StorageFullTreeNode {
	return &StorageFullTreeNode{
		Key:        node.key,
		LatestHash: node.latestHash,
		Versions:   node.versions,
		Children:   node.flatten(4)[1:],
	}
}
