#!/bin/bash
set -e

# Check arguments
TOOL=$1
if [ -z "$TOOL" ]; then
    echo "Usage: $0 <tool>"
    echo "  tool: mysqldef, sqlite3def, mssqldef"
    exit 1
fi

# Validate tool
case "$TOOL" in
    mysqldef|sqlite3def|mssqldef)
        ;;
    *)
        echo "Error: Invalid tool '$TOOL'"
        echo "Valid tools: mysqldef, sqlite3def, mssqldef"
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

# WASM binary path
WASM_BINARY="$PROJECT_ROOT/build/wasip1-wasm/$TOOL.wasm"

if [ ! -f "$WASM_BINARY" ]; then
    echo -e "${YELLOW}Error: $TOOL WASM binary not found at $WASM_BINARY${NC}"
    echo -e "${YELLOW}Please run 'make build-wasm' from the project root${NC}"
    exit 1
fi

# Check for wazero
if ! command -v wazero &> /dev/null; then
    echo -e "${YELLOW}Error: wazero not found in PATH${NC}"
    echo -e "${YELLOW}Please install wazero: go install github.com/tetratelabs/wazero/cmd/wazero@latest${NC}"
    exit 1
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}$TOOL Offline Mode Demo (WASI + Wazero)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

echo -e "${GREEN}Current schema (initial_schema.sql):${NC}"
cat "$SCHEMA_DIR/initial_schema.sql"
echo ""

echo -e "${GREEN}Desired schema (schema.sql):${NC}"
cat "$SCHEMA_DIR/schema.sql"
echo ""

echo -e "${GREEN}1. Compare two schema files (initial_schema.sql → schema.sql)${NC}"
echo -e "${YELLOW}Command: wazero run $TOOL.wasm --file /example/$TOOL/schema.sql /example/$TOOL/initial_schema.sql${NC}"
echo ""
cd "$PROJECT_ROOT"
wazero run -mount=.:/ "$WASM_BINARY" --file "/example/$TOOL/schema.sql" "/example/$TOOL/initial_schema.sql"
echo ""

echo -e "${GREEN}2. Verify idempotency (compare identical schemas)${NC}"
echo -e "${YELLOW}Command: wazero run $TOOL.wasm --file /example/$TOOL/schema.sql /example/$TOOL/schema.sql${NC}"
echo ""
wazero run -mount=.:/ "$WASM_BINARY" --file "/example/$TOOL/schema.sql" "/example/$TOOL/schema.sql"
echo ""

echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Offline mode demo (WASI) completed!${NC}"
echo -e "${BLUE}========================================${NC}"
