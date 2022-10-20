// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package redis

import (
	"github.com/go-redis/redis/v8"
)

// An Option configures a *Database
type Option interface {
	Apply(*Database)
}

// OptionFunc is a function that configures a *Database
type OptionFunc func(*Database)

// Apply is a function that set value to *Database
func (f OptionFunc) Apply(engine *Database) {
	f(engine)
}

func WithHooks(hooks ...redis.Hook) Option {
	return OptionFunc(func(db *Database) {
		if db.db == nil {
			return
		}
		for _, hook := range hooks {
			db.db.AddHook(hook)
		}
	})
}

func WithSharedPipeliner(pipe redis.Pipeliner) Option {
	return OptionFunc(func(db *Database) {
		db.sharedPipe = pipe
	})
}

func NewWithSharedPipeliner() Option {
	return OptionFunc(func(db *Database) {
		if db.db != nil {
			db.sharedPipe = db.db.Pipeline()
		}
	})
}
