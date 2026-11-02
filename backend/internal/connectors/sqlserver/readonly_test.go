package sqlserverconnector

import (
	"strings"
	"testing"
)

func TestReadonlySQLAcceptsSupportedTSQL(t *testing.T) {
	for _, query := range []string{
		"SELECT 1 AS marker", "SELECT @@VERSION", "SELECT TOP (100) * FROM [dbo].[orders] ORDER BY id DESC;",
		"SELECT TOP 5 name FROM sys.tables", "SELECT COUNT(*) FROM dbo.orders WHERE state=N'active'",
		"WITH recent AS (SELECT id FROM dbo.orders WHERE id>1) SELECT * FROM recent",
		"SELECT a.id FROM dbo.orders a LEFT JOIN dbo.items b ON a.id=b.order_id",
		"SELECT CAST(id AS varchar(50)), CONVERT(nvarchar(20),created_at,120) FROM dbo.orders",
		"SELECT ROW_NUMBER() OVER (ORDER BY id) AS n FROM dbo.orders",
		"SELECT * FROM dbo.orders WHERE id IN (SELECT id FROM dbo.items)",
		"SELECT CASE WHEN id>2 THEN 'a' ELSE 'b' END FROM dbo.orders",
		"SELECT 1 UNION ALL SELECT 2", "SELECT '; DROP TABLE dbo.orders' AS sample -- harmless data",
		"SELECT * FROM dbo.[order details] WHERE [select]='x'",
		"SELECT COUNT(DISTINCT state), SUM(id) FROM dbo.orders GROUP BY state HAVING COUNT(*)>1",
		"SELECT id FROM dbo.orders WHERE name LIKE N'a%' ESCAPE '!' AND state IS NOT NULL",
		"SELECT SUM(id) OVER (PARTITION BY state ORDER BY id ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) FROM dbo.orders",
		"SELECT TOP (10) WITH TIES * FROM dbo.orders ORDER BY id",
		"SELECT * FROM (SELECT id FROM dbo.orders) AS x(id)",
		"SELECT LAG(id,1) OVER (ORDER BY id) FROM dbo.orders",
	} {
		t.Run(query, func(t *testing.T) {
			if err := validateReadonlySQL(query); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReadonlySQLRejectsUnsafeAndUnsupportedTSQL(t *testing.T) {
	for _, query := range []string{
		"", "SELECT 1; DROP TABLE dbo.orders", "SELECT 1 SELECT 2", "SELECT * INTO dbo.stolen FROM dbo.orders",
		"WITH c AS (SELECT 1 AS id) DELETE FROM dbo.orders", "EXEC xp_cmdshell 'whoami'", "SELECT NEXT VALUE FOR dbo.seq",
		"SELECT * FROM OPENROWSET('x','y','z')", "SELECT * FROM OPENQUERY(server,'SELECT 1')",
		"SELECT dbo.custom()", "SELECT custom()", "SELECT * FROM dbo.custom()",
		"SELECT * FROM server.db.dbo.orders", "SELECT * FROM otherdb.dbo.orders",
		"SELECT * FROM dbo.orders WITH (UPDLOCK)", "SELECT * FROM dbo.orders OPTION (RECOMPILE)",
		"SELECT id FROM dbo.orders ORDER BY dbo.custom()", "SELECT * FROM dbo.orders WHERE EXISTS (SELECT dbo.custom())",
		"SELECT ROW_NUMBER() OVER (ORDER BY dbo.custom()) FROM dbo.orders", "WITH c AS (SELECT dbo.custom() AS x) SELECT * FROM c",
		"SELECT @variable=1", "SELECT @@ROWCOUNT", "SELECT * FROM #temp", "SELECT CAST('x' AS dbo.CustomType)",
		"SELECT * FROM dbo.orders FOR XML PATH", "SELECT 1\x00; DELETE FROM dbo.orders", "SELECT " + strings.Repeat("(", 33) + "1" + strings.Repeat(")", 33),
	} {
		t.Run(query, func(t *testing.T) {
			if err := validateReadonlySQL(query); err == nil {
				t.Fatal("unsafe SQL accepted")
			}
		})
	}
}
