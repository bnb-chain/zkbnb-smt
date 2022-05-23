package bsmt

import (
	"hash"

	"github.com/bnb-chain/bas-smt/database"
	"github.com/ethereum/go-ethereum/rlp"
)

func recoveryStorageFullTreeNode(db database.TreeDB, key []byte) (*StorageFullTreeNode, error) {
	buf, err := db.Get(storageFullTreeNodeKey(uint64(len(key)), key))
	if err != nil {
		return nil, err
	}
	storageFullNode := &StorageFullTreeNode{}
	err = rlp.DecodeBytes(buf, storageFullNode)
	if err != nil {
		return nil, err
	}
	return storageFullNode, nil
}

type StorageFullTreeNode struct {
	Key        []byte
	LatestHash []byte
	Versions   []Version
	Children   []*StorageShortTreeNode
}

func (node *StorageFullTreeNode) ToFullTreeNode(hasher hash.Hash, db database.TreeDB, key []byte) *FullTreeNode {
	depth := uint64(len(key))
	hash := node.LatestHash
	tmp := make(map[string]*FullTreeNode, len(node.Children)+1)
	head := NewFullNode(hasher, key, hash, node.Versions, depth, db)
	tmp[string(head.key)] = head
	for _, child := range node.Children {
		key = child.Key
		depth := uint64(len(key))
		hash := child.LatestHash

		parentKey := key[:len(key)-1]
		childNode := NewFullNode(hasher, key, hash, child.Versions, depth, db)
		tmp[string(childNode.key)] = childNode
		switch child.Key[len(key)-1] {
		case left:
			tmp[string(parentKey)].leftChild = childNode
		case right:
			tmp[string(parentKey)].rightChild = childNode
		}
	}

	return head
}

type StorageShortTreeNode struct {
	Key        []byte
	LatestHash []byte
	Versions   []Version
}
