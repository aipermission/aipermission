package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/commandrequests"
)

func (s *Server) getCommandRequest(
	ctx context.Context,
	runtime *databaseRuntime,
	id int64,
	tokenID int64,
	source string,
) (commandRequestRecord, error) {
	return commandrequests.NewStore(runtime.database).Get(ctx, id, tokenID, source)
}
