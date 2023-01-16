// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package bsmt

import (
	"github.com/pkg/errors"
)

var (
	ErrEmptyRoot = errors.New("empty root")

	ErrVersionTooOld = errors.New("the version is lower than the rollback version")

	ErrVersionTooHigh = errors.New("the version is higher than the latest version")

	ErrVersionTooLow = errors.New("the version is lower than the latest version")

	ErrNodeNotFound = errors.New("tree node not found")

	ErrVersionMismatched = errors.New("the version is mismatched with the database")

	ErrUnexpected = errors.New("unexpected error")

	ErrInvalidKey = errors.New("invalid key")

	ErrInvalidDepth = errors.New("depth must be a multiple of 4")

	ErrExtendNode = errors.New("extending node error")
)
