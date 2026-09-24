package rabbitmqconnector

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

const (
	maxRabbitListPageSize  = 500
	maxBindingScanPages    = 20
	rabbitQueueListColumns = "name,vhost,durable,auto_delete,exclusive,state,messages,messages_ready,messages_unacknowledged,consumers,memory,idle_since"
)

type rabbitListPage struct {
	Page          int              `json:"page"`
	PageSize      int              `json:"page_size"`
	PageCount     int              `json:"page_count"`
	FilteredCount int              `json:"filtered_count"`
	Items         []map[string]any `json:"items"`
}

func readRabbitListPage(ctx context.Context, client *rabbitClient, path string, page int, pageSize int, query url.Values) (rabbitListPage, error) {
	params := url.Values{}
	for key, values := range query {
		params[key] = append([]string(nil), values...)
	}
	params.Set("page", strconv.Itoa(page))
	params.Set("page_size", strconv.Itoa(pageSize))
	params.Set("pagination", "true")
	var result rabbitListPage
	if err := client.Get(ctx, path+"?"+params.Encode(), &result); err != nil {
		return rabbitListPage{}, err
	}
	if result.Page != page || result.PageSize != pageSize || result.PageCount < 0 || result.FilteredCount < 0 || len(result.Items) > pageSize || result.FilteredCount < len(result.Items) {
		return rabbitListPage{}, fmt.Errorf("rabbitmq returned an invalid paginated list response")
	}
	if result.PageCount == 0 && len(result.Items) > 0 {
		return rabbitListPage{}, fmt.Errorf("rabbitmq returned items without a page count")
	}
	if result.PageCount > 0 && page > result.PageCount {
		return rabbitListPage{}, fmt.Errorf("rabbitmq returned a page beyond the final page")
	}
	return result, nil
}

func collectRabbitList(ctx context.Context, client *rabbitClient, path string, query url.Values, limit int, pageSize int, maxPages int, accept func(map[string]any) bool) ([]map[string]any, bool, bool, error) {
	rows := make([]map[string]any, 0, limit)
	for pageNumber := 1; pageNumber <= maxPages; pageNumber++ {
		page, err := readRabbitListPage(ctx, client, path, pageNumber, pageSize, query)
		if err != nil {
			return nil, false, false, err
		}
		for _, row := range page.Items {
			if accept(row) {
				rows = append(rows, row)
			}
			if len(rows) > limit {
				return rows[:limit], true, false, nil
			}
		}
		if pageNumber >= page.PageCount {
			return rows, false, false, nil
		}
		if len(page.Items) == 0 {
			return nil, false, false, fmt.Errorf("rabbitmq returned an empty list page before the final page")
		}
		if pageNumber == maxPages {
			return rows, true, true, nil
		}
	}
	return rows, false, false, nil
}
