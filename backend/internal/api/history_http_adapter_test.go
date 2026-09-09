package api

import (
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
)

type historyEntryRecord = historypkg.Entry
type historyLabelRecord = historypkg.Label
type historyPageResponse = historypkg.PageResponse
type createHistoryLabelRequest = historypkg.CreateLabelRequest
type attachHistoryLabelRequest = historypkg.AttachLabelRequest
