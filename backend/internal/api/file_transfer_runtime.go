package api

import "context"

func (rt *databaseRuntime) registerTransferCancel(id int64, cancel context.CancelFunc) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	if rt.transferCancels == nil {
		rt.transferCancels = map[int64]context.CancelFunc{}
	}
	rt.transferCancels[id] = cancel
}

func (rt *databaseRuntime) unregisterTransferCancel(id int64) {
	rt.transferMu.Lock()
	defer rt.transferMu.Unlock()
	delete(rt.transferCancels, id)
}

func (rt *databaseRuntime) cancelTransfer(id int64) bool {
	rt.transferMu.Lock()
	cancel := rt.transferCancels[id]
	rt.transferMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}
