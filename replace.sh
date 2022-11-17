#!/usr/bin/env bash

DEPLOY_PATH=~/zkbnb-deploy

cd ${DEPLOY_PATH}/zkbnb
if [[ "$1" == "r" ]]; then
  git checkout -- ${DEPLOY_PATH}/zkbnb/go.mod
  sed -i -e "s/MultiUpdate/MultiSet/g" ${DEPLOY_PATH}/zkbnb/core/statedb/statedb.go
  go mod tidy
  pm2 restart committer
  exit 0
fi

go mod edit -replace github.com/bnb-chain/zkbnb-smt=/Users/damon/GolandProjects/bnb-chain/zkbnb-smt
go mod tidy
sed -i -e "s/MultiSet/MultiUpdate/g" ${DEPLOY_PATH}/zkbnb/core/statedb/statedb.go
pm2 restart committer
