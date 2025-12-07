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
RED='\033[0;31m'
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

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}$TOOL Database Mode Demo${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo -e "${RED}Error: $TOOL binary not found at $BINARY${NC}"
    echo -e "${YELLOW}Please run 'make build' from the project root${NC}"
    exit 1
fi

# Tool-specific setup
case "$TOOL" in
    psqldef)
        USER="postgres"
        DB="psqldef_example"

        # Check database connection
        echo -e "${GREEN}1. Checking PostgreSQL connection...${NC}"
        if ! psql -U "$USER" -c "SELECT 1;" > /dev/null 2>&1; then
            echo -e "${RED}Error: Cannot connect to PostgreSQL${NC}"
            echo -e "${YELLOW}Please ensure PostgreSQL is running and accessible${NC}"
            exit 1
        fi
        echo ""

        # Create database
        echo -e "${GREEN}2. Creating database $DB...${NC}"
        echo -e "${YELLOW}Command: psql -U $USER -c \"DROP DATABASE IF EXISTS $DB;\"${NC}"
        psql -U "$USER" -c "DROP DATABASE IF EXISTS $DB;" 2>/dev/null || true
        echo -e "${YELLOW}Command: psql -U $USER -c \"CREATE DATABASE $DB;\"${NC}"
        psql -U "$USER" -c "CREATE DATABASE $DB;"
        echo ""

        # Create initial schema
        echo -e "${GREEN}3. Creating initial schema...${NC}"
        echo -e "${YELLOW}Command: psql -U $USER $DB < initial_schema.sql${NC}"
        psql -U "$USER" "$DB" < "$SCHEMA_DIR/initial_schema.sql"
        echo ""

        # Export current schema
        echo -e "${GREEN}4. Exporting current schema${NC}"
        echo -e "${YELLOW}Command: psqldef -U $USER $DB --export${NC}"
        echo ""
        "$BINARY" -U "$USER" "$DB" --export
        echo ""

        # Preview changes (dry run)
        echo -e "${GREEN}5. Preview changes (dry run)${NC}"
        echo -e "${YELLOW}Command: psqldef -U $USER $DB --dry-run < schema.sql${NC}"
        echo ""
        "$BINARY" -U "$USER" "$DB" --dry-run < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Apply changes
        echo -e "${GREEN}6. Applying schema changes${NC}"
        echo -e "${YELLOW}Command: psqldef -U $USER $DB --apply < schema.sql${NC}"
        echo ""
        "$BINARY" -U "$USER" "$DB" --apply < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Verify idempotency
        echo -e "${GREEN}7. Verifying idempotency (running again)${NC}"
        echo -e "${YELLOW}Command: psqldef -U $USER $DB --apply < schema.sql${NC}"
        echo ""
        "$BINARY" -U "$USER" "$DB" --apply < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Export final schema
        echo -e "${GREEN}8. Final schema${NC}"
        echo -e "${YELLOW}Command: psqldef -U $USER $DB --export${NC}"
        echo ""
        "$BINARY" -U "$USER" "$DB" --export
        echo ""

        CLEANUP_CMD="psql -U $USER -c \"DROP DATABASE $DB;\""
        ;;

    mysqldef)
        USER="root"
        DB="mysqldef_example"

        # Check database connection
        echo -e "${GREEN}1. Checking MySQL connection...${NC}"
        if ! mysql -u"$USER" -e "SELECT 1;" > /dev/null 2>&1; then
            echo -e "${RED}Error: Cannot connect to MySQL${NC}"
            echo -e "${YELLOW}Please ensure MySQL is running and accessible${NC}"
            exit 1
        fi
        echo ""

        # Create database
        echo -e "${GREEN}2. Creating database $DB...${NC}"
        echo -e "${YELLOW}Command: mysql -u$USER -e \"DROP DATABASE IF EXISTS $DB;\"${NC}"
        mysql -u"$USER" -e "DROP DATABASE IF EXISTS $DB;" 2>/dev/null || true
        echo -e "${YELLOW}Command: mysql -u$USER -e \"CREATE DATABASE $DB;\"${NC}"
        mysql -u"$USER" -e "CREATE DATABASE $DB;"
        echo ""

        # Create initial schema
        echo -e "${GREEN}3. Creating initial schema...${NC}"
        echo -e "${YELLOW}Command: mysql -u$USER $DB < initial_schema.sql${NC}"
        mysql -u"$USER" "$DB" < "$SCHEMA_DIR/initial_schema.sql"
        echo ""

        # Export current schema
        echo -e "${GREEN}4. Exporting current schema${NC}"
        echo -e "${YELLOW}Command: mysqldef -u$USER $DB --export${NC}"
        echo ""
        "$BINARY" -u"$USER" "$DB" --export
        echo ""

        # Preview changes (dry run)
        echo -e "${GREEN}5. Preview changes (dry run)${NC}"
        echo -e "${YELLOW}Command: mysqldef -u$USER $DB --dry-run < schema.sql${NC}"
        echo ""
        "$BINARY" -u"$USER" "$DB" --dry-run < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Apply changes
        echo -e "${GREEN}6. Applying schema changes${NC}"
        echo -e "${YELLOW}Command: mysqldef -u$USER $DB --apply < schema.sql${NC}"
        echo ""
        "$BINARY" -u"$USER" "$DB" --apply < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Verify idempotency
        echo -e "${GREEN}7. Verifying idempotency (running again)${NC}"
        echo -e "${YELLOW}Command: mysqldef -u$USER $DB --apply < schema.sql${NC}"
        echo ""
        "$BINARY" -u"$USER" "$DB" --apply < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Export final schema
        echo -e "${GREEN}8. Final schema${NC}"
        echo -e "${YELLOW}Command: mysqldef -u$USER $DB --export${NC}"
        echo ""
        "$BINARY" -u"$USER" "$DB" --export
        echo ""

        CLEANUP_CMD="mysql -u$USER -e \"DROP DATABASE $DB;\""
        ;;

    sqlite3def)
        DB="$SCHEMA_DIR/example.db"

        # Check if sqlite3 is installed
        echo -e "${GREEN}1. Checking SQLite3 installation...${NC}"
        if ! command -v sqlite3 &> /dev/null; then
            echo -e "${RED}Error: sqlite3 is not installed${NC}"
            echo -e "${YELLOW}Please install SQLite3${NC}"
            exit 1
        fi
        echo ""

        # Remove existing database
        echo -e "${GREEN}2. Creating new database...${NC}"
        echo -e "${YELLOW}Command: rm -f $DB${NC}"
        rm -f "$DB"
        echo ""

        # Create initial schema
        echo -e "${GREEN}3. Creating initial schema...${NC}"
        echo -e "${YELLOW}Command: sqlite3 $DB < initial_schema.sql${NC}"
        sqlite3 "$DB" < "$SCHEMA_DIR/initial_schema.sql"
        echo ""

        # Export current schema
        echo -e "${GREEN}4. Exporting current schema${NC}"
        echo -e "${YELLOW}Command: sqlite3def $DB --export${NC}"
        echo ""
        "$BINARY" "$DB" --export
        echo ""

        # Preview changes (dry run)
        echo -e "${GREEN}5. Preview changes (dry run)${NC}"
        echo -e "${YELLOW}Command: sqlite3def $DB --dry-run < schema.sql${NC}"
        echo ""
        "$BINARY" "$DB" --dry-run < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Apply changes
        echo -e "${GREEN}6. Applying schema changes${NC}"
        echo -e "${YELLOW}Command: sqlite3def $DB --apply < schema.sql${NC}"
        echo ""
        "$BINARY" "$DB" --apply < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Verify idempotency
        echo -e "${GREEN}7. Verifying idempotency (running again)${NC}"
        echo -e "${YELLOW}Command: sqlite3def $DB --apply < schema.sql${NC}"
        echo ""
        "$BINARY" "$DB" --apply < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Export final schema
        echo -e "${GREEN}8. Final schema${NC}"
        echo -e "${YELLOW}Command: sqlite3def $DB --export${NC}"
        echo ""
        "$BINARY" "$DB" --export
        echo ""

        CLEANUP_CMD="rm $DB"
        ;;

    mssqldef)
        USER="sa"
        PASSWORD="Passw0rd"  # Matches compose.yml SA_PASSWORD
        DB="mssqldef_example"
        HOST="localhost"

        # Check if sqlcmd is available (optional, for setup only)
        echo -e "${GREEN}1. Checking SQL Server setup...${NC}"
        if ! command -v sqlcmd &> /dev/null; then
            echo -e "${YELLOW}Warning: sqlcmd not found. Skipping database creation check.${NC}"
            echo -e "${YELLOW}Please ensure the database $DB exists before running this script.${NC}"
        else
            # Create database if it doesn't exist
            echo -e "${GREEN}2. Creating database $DB...${NC}"
            echo -e "${YELLOW}Command: sqlcmd -S $HOST -U $USER -P *** -Q \"DROP DATABASE IF EXISTS $DB;\"${NC}"
            sqlcmd -S "$HOST" -U "$USER" -P "$PASSWORD" -Q "DROP DATABASE IF EXISTS $DB;" 2>/dev/null || true
            echo -e "${YELLOW}Command: sqlcmd -S $HOST -U $USER -P *** -Q \"CREATE DATABASE $DB;\"${NC}"
            sqlcmd -S "$HOST" -U "$USER" -P "$PASSWORD" -Q "CREATE DATABASE $DB;"
            echo ""

            # Create initial schema
            echo -e "${GREEN}3. Creating initial schema...${NC}"
            echo -e "${YELLOW}Command: sqlcmd -S $HOST -U $USER -P *** -d $DB -i initial_schema.sql${NC}"
            sqlcmd -S "$HOST" -U "$USER" -P "$PASSWORD" -d "$DB" -i "$SCHEMA_DIR/initial_schema.sql"
            echo ""
        fi

        # Export current schema
        echo -e "${GREEN}4. Exporting current schema${NC}"
        echo -e "${YELLOW}Command: mssqldef -U $USER -P *** -h $HOST $DB --export${NC}"
        echo ""
        "$BINARY" -U "$USER" -P "$PASSWORD" -h "$HOST" "$DB" --export
        echo ""

        # Preview changes (dry run)
        echo -e "${GREEN}5. Preview changes (dry run)${NC}"
        echo -e "${YELLOW}Command: mssqldef -U $USER -P *** -h $HOST $DB --dry-run < schema.sql${NC}"
        echo ""
        "$BINARY" -U "$USER" -P "$PASSWORD" -h "$HOST" "$DB" --dry-run < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Apply changes
        echo -e "${GREEN}6. Applying schema changes${NC}"
        echo -e "${YELLOW}Command: mssqldef -U $USER -P *** -h $HOST $DB --apply < schema.sql${NC}"
        echo ""
        "$BINARY" -U "$USER" -P "$PASSWORD" -h "$HOST" "$DB" --apply < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Verify idempotency
        echo -e "${GREEN}7. Verifying idempotency (running again)${NC}"
        echo -e "${YELLOW}Command: mssqldef -U $USER -P *** -h $HOST $DB --apply < schema.sql${NC}"
        echo ""
        "$BINARY" -U "$USER" -P "$PASSWORD" -h "$HOST" "$DB" --apply < "$SCHEMA_DIR/schema.sql"
        echo ""

        # Export final schema
        echo -e "${GREEN}8. Final schema${NC}"
        echo -e "${YELLOW}Command: mssqldef -U $USER -P *** -h $HOST $DB --export${NC}"
        echo ""
        "$BINARY" -U "$USER" -P "$PASSWORD" -h "$HOST" "$DB" --export
        echo ""

        CLEANUP_CMD="sqlcmd -S $HOST -U $USER -P '$PASSWORD' -Q \"DROP DATABASE $DB;\""
        ;;
esac

echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Example completed successfully!${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "To cleanup: ${YELLOW}$CLEANUP_CMD${NC}"
echo ""
