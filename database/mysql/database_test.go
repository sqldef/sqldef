package mysql

import (
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/parser"
)

func TestFormatDistributionPolicyDDL(t *testing.T) {
	ddl, err := formatDistributionPolicyDDL(distributionPolicyMetadata{
		Name: "dp_basic",
		Desc: `{"constraints":[{"key":"replica-count","op":"=","values":["3"]}]}`,
	})
	if err != nil {
		t.Fatalf("formatDistributionPolicyDDL returned error: %v", err)
	}
	want := `CREATE DISTRIBUTION POLICY "dp_basic" SET REPLICA_COUNT = 3;`
	if ddl != want {
		t.Fatalf("unexpected DDL:\n got: %s\nwant: %s", ddl, want)
	}
}

func TestFormatDistributionPolicyDDLWithExistsAsFirstConstraintParses(t *testing.T) {
	ddl, err := formatDistributionPolicyDDL(distributionPolicyMetadata{
		Name: "dp_exists_first",
		Desc: `{"constraints":[{"key":"region","op":"exists","values":[]},{"key":"replica-count","op":"=","values":["3"]}]}`,
	})
	if err != nil {
		t.Fatalf("formatDistributionPolicyDDL returned error: %v", err)
	}
	if _, err := parser.ParseDDL(ddl, parser.ParserModeMysql); err != nil {
		t.Fatalf("formatted distribution policy could not be parsed: %v\nDDL: %s", err, ddl)
	}
}

func TestFormatDistributionPolicyDDLWithMultipleConstraints(t *testing.T) {
	ddl, err := formatDistributionPolicyDDL(distributionPolicyMetadata{
		Name: "dp_advanced",
		Desc: `{"constraints":[{"key":"node","op":"in","values":["node-001","node-002"]},{"key":"replica-count","op":"=","values":["2"]},{"key":"region","op":"exists","values":[]}]}`,
	})
	if err != nil {
		t.Fatalf("formatDistributionPolicyDDL returned error: %v", err)
	}
	want := `CREATE DISTRIBUTION POLICY "dp_advanced" SET NODE IN ("node-001", "node-002") AND REPLICA_COUNT = 2 AND REGION EXISTS;`
	if ddl != want {
		t.Fatalf("unexpected DDL:\n got: %s\nwant: %s", ddl, want)
	}
}

func TestDistributionPolicyBindingsQueryUsesPolicyID(t *testing.T) {
	query := strings.ToLower(distributionPolicyBindingsQuery)
	if !strings.Contains(query, "o.distribution_policy_id is not null") {
		t.Fatal("binding query must use distribution_policy_id as the binding signal")
	}
	for _, condition := range []string{"o.partition_name = ''", "o.sub_partition_name = ''", "o.index_name = ''"} {
		if strings.Contains(query, condition) {
			t.Fatalf("binding query must not require %q", condition)
		}
	}
}

func TestDatabaseDistributionPolicyBindingsQueryUsesDatabaseObjects(t *testing.T) {
	query := strings.ToLower(databaseDistributionPolicyBindingsQuery)
	for _, fragment := range []string{
		"o.schema_name",
		"o.table_name is null",
		"o.distribution_policy_id",
		"d.distribution_policy_name",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("database binding query missing %q", fragment)
		}
	}
	if strings.Contains(query, "data_object_name") || strings.Contains(query, "data_object_type = 'database'") {
		t.Fatal("database binding query references columns that are not in META_CLUSTER_DPS")
	}
}

func TestPartitionPolicyBindingsQueryMatchesQualifiedPartitionObjects(t *testing.T) {
	query := strings.ToLower(partitionPolicyBindingsQuery)
	for _, fragment := range []string{
		"lower(trim(hidden)) = 'explicit'",
		"data_object_name = concat(?, '.', ?)",
		"data_object_name like concat(?, '.', ?, '.%')",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("partition binding query missing %q", fragment)
		}
	}
}

func TestAppendUsingPolicy(t *testing.T) {
	ddl := appendUsingPolicy("CREATE TABLE t (id int);", "DISTRIBUTION POLICY", "dp_1")
	want := `CREATE TABLE t (id int) USING DISTRIBUTION POLICY "dp_1";`
	if ddl != want {
		t.Fatalf("unexpected DDL: got %q want %q", ddl, want)
	}
	if got := appendUsingPolicy(want, "DISTRIBUTION POLICY", "dp_1"); got != want {
		t.Fatalf("duplicate USING clause was appended: %q", got)
	}
}

func TestFormatPartitionPolicyDDL(t *testing.T) {
	ddl, ok := formatPartitionPolicyDDL(partitionPolicyMetadata{
		Name:       "pp`quoted",
		Method:     "HASH",
		Expression: "INTEGER",
		Partitions: 4,
	})
	if !ok {
		t.Fatal("expected HASH policy to be exportable")
	}
	want := "CREATE PARTITION POLICY `pp``quoted` PARTITION BY HASH(INTEGER) PARTITIONS 4;"
	if ddl != want {
		t.Fatalf("unexpected DDL:\n got: %s\nwant: %s", ddl, want)
	}
}

func TestFormatPartitionPolicyDDLKey51(t *testing.T) {
	ddl, ok := formatPartitionPolicyDDL(partitionPolicyMetadata{
		Name:        "pp2",
		Method:      "KEY_51",
		PrivateData: "partition_columns_num=1;",
		Partitions:  3,
	})
	if !ok {
		t.Fatal("expected KEY_51 policy to be exportable")
	}
	want := "CREATE PARTITION POLICY `pp2` PARTITION BY KEY COLUMNS 1 PARTITIONS 3;"
	if ddl != want {
		t.Fatalf("unexpected DDL: got %q want %q", ddl, want)
	}
	if _, err := parser.ParseDDL(ddl, parser.ParserModeMysql); err != nil {
		t.Fatalf("formatted KEY policy could not be parsed: %v", err)
	}
}

func TestFormatPartitionPolicyDDLIncludesImplicitPolicy(t *testing.T) {
	ddl, ok := formatPartitionPolicyDDL(partitionPolicyMetadata{
		Name:       "pp_implicit",
		Method:     "HASH",
		Expression: "INT",
		Hidden:     "Implicit",
		Partitions: 2,
	})
	if !ok {
		t.Fatal("expected implicit policy to be exportable")
	}
	if want := "CREATE PARTITION POLICY `pp_implicit` PARTITION BY HASH(INT) PARTITIONS 2;"; ddl != want {
		t.Fatalf("unexpected DDL: got %q want %q", ddl, want)
	}
}

func TestFormatPartitionPolicyDDLNoClause(t *testing.T) {
	ddl, ok := formatPartitionPolicyDDL(partitionPolicyMetadata{Name: "pp_empty"})
	if !ok {
		t.Fatal("expected no-clause policy to be exportable")
	}
	if want := "CREATE PARTITION POLICY `pp_empty`;"; ddl != want {
		t.Fatalf("unexpected DDL: got %q want %q", ddl, want)
	}
}

func TestFormatPartitionPolicyDDLRejectsUnsupportedMethod(t *testing.T) {
	if ddl, ok := formatPartitionPolicyDDL(partitionPolicyMetadata{
		Name:       "pp_range",
		Method:     "RANGE",
		Expression: "id",
		Partitions: 4,
	}); ok || ddl != "" {
		t.Fatalf("expected RANGE policy to be skipped, got ddl=%q ok=%v", ddl, ok)
	}
}
