package sqlserverconnector

import "strings"

var readServerVariables = map[string]bool{"@@version": true, "@@servername": true, "@@lock_timeout": true, "@@textsize": true}

var readFunctions = wordSet(`abs avg ceiling coalesce concat concat_ws count count_big dateadd datediff datediff_big datename datepart day db_name dense_rank
 floor format getdate getutcdate isdate isjson isnull isnumeric json_query json_value lag lead left len lower ltrim max min month nullif object_name
 replace reverse right round row_number rtrim schema_name serverproperty string_agg substring sum sysdatetime sysutcdatetime trim upper year
 cast try_cast convert try_convert`)

var readTypes = wordSet(`bigint int smallint tinyint bit decimal numeric money smallmoney float real date datetime datetime2 datetimeoffset smalldatetime time
 char varchar nchar nvarchar binary varbinary uniqueidentifier`)

func wordSet(words string) map[string]bool {
	result := map[string]bool{}
	for _, word := range strings.Fields(words) {
		result[word] = true
	}
	return result
}
