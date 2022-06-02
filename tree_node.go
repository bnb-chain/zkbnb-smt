package bsmt

import (
	"hash"

	"github.com/bnb-chain/bas-smt/utils"
)

func NewTreeNode(depth uint8, path uint64, hasher hash.Hash) *TreeNode {
	fullNode := &TreeNode{
		path:   path,
		depth:  depth,
		hasher: hasher,
	}
	depthItr := uint8(0)
	pathItr := uint64(0)
	elementNum := 1 << depthItr
	fullNode.Children[0] = &LeafNode{
		LatestHash: emptyHash,
		depth:      depth,
		path:       path,
	}
	for i := 1; i < 15; i++ {
		if i == elementNum {
			depthItr++
			pathItr = 0
			elementNum += 1 << depthItr
		}
		fullNode.Children[i] = &LeafNode{
			LatestHash: emptyHash,
			depth:      depth + depthItr,
			path:       path*(1<<(depth+depthItr-1)) + pathItr,
		}
		pathItr++
	}
	return fullNode
}

type TreeNode struct {
	Children [15]*LeafNode

	path   uint64    `rlp:"-"`
	depth  uint8     `rlp:"-"`
	hasher hash.Hash `rlp:"-"`
}

func (node *TreeNode) Rebuild() {
	depthItr := uint8(0)
	pathItr := uint64(0)
	elementNum := 1 << depthItr
	node.Children[0].depth = node.depth
	node.Children[0].path = node.path
	for i := 1; i < 15; i++ {
		if i == elementNum {
			pathItr = 0
			depthItr++
			elementNum += 1 << depthItr
		}
		depth := node.depth + depthItr
		node.Children[i].depth = depth
		node.Children[i].path = node.path*(1<<(depth-1)) + pathItr
		pathItr++
	}
}

func (node *TreeNode) Root() []byte {
	if node.Children[0] == nil {
		return emptyHash
	}
	return node.Children[0].Root()
}

func (node *TreeNode) index(key uint64) int {
	return int(key - 1<<node.depth + 1 - node.path)
}

func (node *TreeNode) Get(key uint64) (*LeafNode, error) {
	return node.Children[node.index(key)], nil
}

func (node *TreeNode) Set(key uint64, val []byte, version Version) *TreeNode {
	copied := node.Copy()
	index := node.index(key)
	copied.Children[index].Set(val, version)
	// recompute the parent hash
	parentKey := int(key-1) >> 1
	index = node.index(uint64(parentKey))
	for index > 0 {
		parentChild := copied.Children[index]
		inc := 0
		if parentChild.depth > 0 {
			inc = 1 << (parentChild.depth - 1)
		}
		leftKey := uint64(parentKey + inc + int(parentChild.path))
		left := copied.Children[node.index(leftKey)]
		rightKey := leftKey + 1
		right := copied.Children[node.index(rightKey)]
		copied.Children[index].Set(copied.HashSubTree(left, right), version)
		parentKey = (parentKey - 1) >> 1
		index = node.index(uint64(parentKey))
	}
	return copied
}

func (node *TreeNode) Key() uint64 {
	if node.depth == 0 {
		return 0
	}
	return uint64(1<<node.depth - 1 + node.path)
}

func (node *TreeNode) HashSubTree(left, right *LeafNode) []byte {
	node.hasher.Write(left.Root())
	node.hasher.Write(right.Root())
	defer node.hasher.Reset()
	return node.hasher.Sum(nil)
}

func (node *TreeNode) Copy() *TreeNode {
	var copyChildren [15]*LeafNode
	for i, child := range node.Children {
		copyChildren[i] = child.Copy()
	}
	return &TreeNode{
		Children: copyChildren,
		path:     node.path,
		depth:    node.depth,
		hasher:   node.hasher,
	}
}

func (node *TreeNode) Prune(oldestVersion Version, prune func(depth uint8, path uint64, ver Version) error) error {
	for _, child := range node.Children {
		i := len(child.Versions) - 1
		for ; i >= 0; i-- {
			if child.Versions[i].Ver >= oldestVersion {
				break
			}
			if err := prune(child.depth, child.path, child.Versions[i].Ver); err != nil {
				return err
			}
		}
		if i >= 0 {
			child.Versions = child.Versions[:i+1]
		} else {
			child.Versions = nil
		}
	}
	return nil
}

func (node *TreeNode) Rollback(targetVersion Version, delete func(depth uint8, path uint64, ver Version) error) (bool, error) {
	next := false
	for _, child := range node.Children {
		i := 0
		for ; i < len(child.Versions); i++ {
			if child.Versions[i].Ver <= targetVersion {
				break
			}
			if err := delete(child.depth, child.path, child.Versions[i].Ver); err != nil {
				return false, err
			}
			next = true
		}
		child.Versions = child.Versions[i:]
	}
	return next, nil
}

type VersionInfo struct {
	Ver  Version
	Hash []byte
}

func (info *VersionInfo) Copy() *VersionInfo {
	return &VersionInfo{
		Ver:  info.Ver,
		Hash: utils.CopyBytes(info.Hash),
	}
}

type LeafNode struct {
	LatestHash []byte
	Versions   []*VersionInfo

	path  uint64 `rlp:"-"` // 16種變化
	depth uint8  `rlp:"-"` // 深度
}

func (leaf *LeafNode) Root() []byte {
	return leaf.LatestHash
}

func (leaf *LeafNode) Set(val []byte, version Version) {
	leaf.Versions = append([]*VersionInfo{
		{Ver: version, Hash: val},
	}, leaf.Versions...)
	leaf.LatestHash = val
}

func (leaf *LeafNode) Copy() *LeafNode {
	copyVersions := make([]*VersionInfo, len(leaf.Versions))
	for i, ver := range leaf.Versions {
		copyVersions[i] = ver.Copy()
	}
	return &LeafNode{
		LatestHash: leaf.LatestHash,
		Versions:   copyVersions,
		path:       leaf.path,
		depth:      leaf.depth,
	}
}
