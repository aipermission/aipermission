package api

// API integration tests exercise the generic connector protocol. Concrete
// connector behavior belongs to connector-owned conformance suites.
const (
	testSSHConnectorKind          = "ssh"
	testSSHExecAction             = "exec"
	testSSHRuntimeCapability      = "ssh"
	testPostgresConnectorKind     = "postgres"
	testPostgresGetSchemasAction  = "get_schemas"
	testPostgresGetTablesAction   = "get_tables"
	testPostgresDescribeAction    = "describe_table"
	testPostgresReadonlySQLAction = "query_readonly"
)
