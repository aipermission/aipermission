package connectors

import (
	"context"
	"io"
)

// ReaderWithContext stops reads when ctx is canceled.
func ReaderWithContext(ctx context.Context, source io.Reader) io.Reader {
	return contextReader{ctx: ctx, source: source}
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	read, err := reader.source.Read(buffer)
	if err == nil {
		err = reader.ctx.Err()
	}
	return read, err
}
