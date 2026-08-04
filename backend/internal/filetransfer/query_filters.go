package filetransfer

import "strings"

func listWhere(filter ListFilter) (string, []any) {
	clauses := []string{}
	args := []any{}
	if filter.Direction != "" {
		clauses = append(clauses, "ft.direction = ?")
		args = append(args, filter.Direction)
	}
	if filter.Status != "" {
		clauses = append(clauses, "ft.status = ?")
		args = append(args, filter.Status)
	}
	if filter.RuntimeID > 0 {
		clauses = append(clauses, "ft.runtime_id = ?")
		args = append(args, filter.RuntimeID)
	}
	if len(filter.TargetIDs) > 0 {
		clauses = append(clauses, "rs.target_id IN ("+placeholders(len(filter.TargetIDs))+")")
		for _, id := range filter.TargetIDs {
			args = append(args, id)
		}
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		clauses = append(clauses, "(ft.remote_path LIKE ? OR ft.local_path LIKE ? OR ft.file_name LIKE ? OR COALESCE(ct.name, '') LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func batchListWhere(filter BatchListFilter) (string, []any) {
	clauses := []string{}
	args := []any{}
	if filter.Direction != "" {
		clauses = append(clauses, "b.direction = ?")
		args = append(args, filter.Direction)
	}
	if filter.Status != "" {
		clauses = append(clauses, "b.status = ?")
		args = append(args, filter.Status)
	}
	if filter.RuntimeID > 0 {
		clauses = append(clauses, "b.runtime_id = ?")
		args = append(args, filter.RuntimeID)
	}
	if len(filter.TargetIDs) > 0 {
		clauses = append(clauses, "rs.target_id IN ("+placeholders(len(filter.TargetIDs))+")")
		for _, id := range filter.TargetIDs {
			args = append(args, id)
		}
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		clauses = append(clauses, "(b.archive_name LIKE ? OR COALESCE(ct.name, '') LIKE ?)")
		args = append(args, like, like)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func placeholders(count int) string {
	if count < 1 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = "?"
	}
	return strings.Join(items, ",")
}
