#!/bin/bash
set -e

# Check arguments
TOOL=$1
if [ -z "$TOOL" ]; then
    echo "Usage: $0 <tool>"
    echo "  tool: psqldef, mysqldef, sqlite3def, mssqldef"
    exit 1
fi

# Validate tool
case "$TOOL" in
    psqldef|mysqldef|sqlite3def|mssqldef)
        ;;
    *)
        echo "Error: Invalid tool '$TOOL'"
        echo "Valid tools: psqldef, mysqldef, sqlite3def, mssqldef"
        exit 1
        ;;
esac

# Determine script and project directories
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SCHEMA_DIR="$SCRIPT_DIR/$TOOL"

# Color output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Detect OS and architecture for binary path
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
    ARCH="amd64"
elif [ "$ARCH" = "aarch64" ]; then
    ARCH="arm64"
fi

BINARY="$PROJECT_ROOT/build/${OS}-${ARCH}/$TOOL"

if [ ! -f "$BINARY" ]; then
    echo -e "${YELLOW}Error: $TOOL binary not found at $BINARY${NC}"
    echo -e "${YELLOW}Please run 'make build' from the project root${NC}"
    exit 1
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}$TOOL Offline Mode Demo${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

echo -e "${GREEN}Current schema (initial_schema.sql):${NC}"
cat "$SCHEMA_DIR/initial_schema.sql"
echo ""

echo -e "${GREEN}Desired schema (schema.sql):${NC}"
cat "$SCHEMA_DIR/schema.sql"
echo ""

echo -e "${GREEN}1. Compare two schema files (initial_schema.sql → schema.sql)${NC}"
echo -e "${YELLOW}Command: $TOOL initial_schema.sql < schema.sql${NC}"
echo ""
$BINARY "$SCHEMA_DIR/initial_schema.sql" < "$SCHEMA_DIR/schema.sql"
echo ""

echo -e "${GREEN}2. Same comparison using --file flag${NC}"
echo -e "${YELLOW}Command: $TOOL --file schema.sql initial_schema.sql${NC}"
echo ""
$BINARY --file "$SCHEMA_DIR/schema.sql" "$SCHEMA_DIR/initial_schema.sql"
echo ""

echo -e "${GREEN}3. Verify idempotency (compare identical schemas)${NC}"
echo -e "${YELLOW}Command: $TOOL schema.sql < schema.sql${NC}"
echo ""
$BINARY "$SCHEMA_DIR/schema.sql" < "$SCHEMA_DIR/schema.sql"
echo ""

echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Offline mode demo completed!${NC}"
echo -e "${BLUE}========================================${NC}"
