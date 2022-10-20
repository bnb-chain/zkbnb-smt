// Copyright 2022 bnb-chain. All Rights Reserved.
//
// Distributed under MIT license.
// See file LICENSE for detail or copy at https://opensource.org/licenses/MIT

package leveldb

import (
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"

	"github.com/bnb-chain/zkbnb-smt/database"
	"github.com/bnb-chain/zkbnb-smt/database/dbtest"
)

func TestLevelDB(t *testing.T) {
	t.Run("DatabaseSuite", func(t *testing.T) {
		dbtest.TestDatabaseSuite(t, func() database.TreeDB {
			db, err := leveldb.Open(storage.NewMemStorage(), nil)
			if err != nil {
				t.Fatal(err)
			}
			return NewFromExistLevelDB(db)
		})
	})
}

func TestLevelDBWithNamespace(t *testing.T) {
	t.Run("DatabaseSuite", func(t *testing.T) {
		dbtest.TestDatabaseSuite(t, func() database.TreeDB {
			db, err := leveldb.Open(storage.NewMemStorage(), nil)
			if err != nil {
				t.Fatal(err)
			}

			return WrapWithNamespace(&Database{
				db: db,
			}, "test")
		})
	})
}
