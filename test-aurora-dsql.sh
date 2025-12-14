#!/bin/bash
# wrapper script to connect to Aurora DSQL with psqldef
set -eu -o pipefail

DSQL_CLUSTER_INDEX=0

DSQL_ID=$(aws dsql list-clusters | jq -r ".clusters[$DSQL_CLUSTER_INDEX].identifier")
PGHOST=$(aws dsql get-cluster --identifier $DSQL_ID  | jq -r .endpoint)
PSUSER=admin
PSDATABASE=postgres

PGPASSWORD=$(aws dsql generate-db-connect-admin-auth-token --expires-in 600 --hostname "$PGHOST")

PSQLDEF_PATH="./build/$(go env GOOS)-$(go env GOARCH)/psqldef"

echo PGHOST="$PGHOST"
PGPASSWORD="$PGPASSWORD" $PSQLDEF_PATH --host "$PGHOST" -U $PSUSER $PSDATABASE "$@"
