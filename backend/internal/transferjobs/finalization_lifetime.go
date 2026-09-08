package transferjobs

import "context"

// FinalizationLifetime keeps local outcome persistence alive after a remote
// operation context ends, while still bounding it to the workspace lifetime.
type FinalizationLifetime struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func NewFinalizationLifetime() FinalizationLifetime {
	ctx, cancel := context.WithCancel(context.Background())
	return FinalizationLifetime{ctx: ctx, cancel: cancel}
}

func (l FinalizationLifetime) Context() context.Context {
	if l.ctx == nil {
		return context.Background()
	}
	return l.ctx
}

func (l FinalizationLifetime) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
}
