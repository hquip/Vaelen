#!/usr/bin/env bash
set -euo pipefail

# These runners are disposable; all instance data and ports are allocated by the
# Go integration test. Existing package clusters/databases are never selected.
case "$VSM_TEST_DATABASE_ENGINE" in
  postgres)
    sudo apt-get update -o Acquire::Retries=3
    sudo apt-get install -y --no-install-recommends postgresql-16 postgresql-client-16
    export VSM_TEST_NATIVE_DATABASE_PATH=/usr/lib/postgresql/16
    ;;
  mysql)
    sudo apt-get update -o Acquire::Retries=3
    sudo apt-get install -y --no-install-recommends mysql-server mysql-client
    export VSM_TEST_NATIVE_DATABASE_PATH=/usr/sbin
    ;;
  mariadb)
    sudo apt-get update -o Acquire::Retries=3
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends mariadb-server mariadb-client
    export VSM_TEST_NATIVE_DATABASE_PATH=/usr/sbin
    ;;
  redis)
    sudo apt-get update -o Acquire::Retries=3
    sudo apt-get install -y --no-install-recommends redis-server redis-tools
    export VSM_TEST_NATIVE_DATABASE_PATH=/usr/bin
    ;;
  valkey)
    target="$(mktemp -d)"
    git clone --depth 1 --branch 8.1.0 https://github.com/valkey-io/valkey.git "$target/valkey"
    make -C "$target/valkey" -j2 MALLOC=libc BUILD_TLS=no
    export VSM_TEST_NATIVE_DATABASE_PATH="$target/valkey/src"
    ;;
  *) echo "Unsupported database test engine" >&2; exit 1 ;;
esac
go test ./internal/database -run '^TestDatabaseNativeIntegration$' -v -count=1 -timeout 10m
