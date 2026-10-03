package commandrequests

import (
	"context"
	"errors"
)

const commandObservationUnknown = "command may have been dispatched, but its outcome is unknown; inspect the existing console session and external state before retrying"

func unknownCommandCompletion(id, sessionID int64, detail string) Completion {
	message := commandObservationUnknown
	if detail != "" {
		message = detail + "; " + message
	}
	return Completion{ID: id, Status: "outcome_unknown", SessionID: sessionID, Error: message}
}

func uncertainCompletion(completion Completion) Completion {
	message := commandOutcomeUnknown
	if completion.Status == "outcome_unknown" {
		message = completion.Error
	}
	return Completion{ID: completion.ID, Status: "outcome_unknown", SessionID: completion.SessionID, Error: message}
}

func (r *Runtime) retainPendingCompletion(completion Completion) {
	r.workerMu.Lock()
	defer r.workerMu.Unlock()
	if r.pendingCompletions == nil {
		r.pendingCompletions = make(map[int64]Completion)
	}
	r.pendingCompletions[completion.ID] = uncertainCompletion(completion)
}

func (r *Runtime) recoverPendingCompletions(ctx context.Context) error {
	r.workerMu.Lock()
	pending := make([]Completion, 0, len(r.pendingCompletions))
	for _, completion := range r.pendingCompletions {
		pending = append(pending, completion)
	}
	r.workerMu.Unlock()
	var failures error
	for _, completion := range pending {
		err := r.store.Finish(ctx, r.projection, completion)
		if errors.Is(err, ErrNotRunning) {
			status, statusErr := r.store.Status(ctx, completion.ID)
			if statusErr == nil && status != "running" {
				err = nil
			} else {
				err = errors.Join(err, statusErr)
			}
		}
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		r.clearWorkerError(completion.ID)
	}
	return failures
}
