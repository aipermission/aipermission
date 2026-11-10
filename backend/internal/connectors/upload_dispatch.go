package connectors

import "context"

type uploadDispatchKey struct{}
type stagedUploadKey struct{}

// FileUploadAuthorization returns a context bounded by the current grant
// expiry and a release for the short authorization admission.
type FileUploadAuthorization func(context.Context) (context.Context, func(), error)

func WithStagedUpload(ctx context.Context) context.Context {
	return context.WithValue(ctx, stagedUploadKey{}, true)
}

func StagedUploadAdmitted(ctx context.Context) bool {
	value, _ := ctx.Value(stagedUploadKey{}).(bool)
	return value
}

type uploadDispatch struct {
	ref       string
	authorize FileUploadAuthorization
}

// WithUploadDispatch binds a staged upload reference to a dispatched action.
func WithUploadDispatch(ctx context.Context, ref string, authorize FileUploadAuthorization) context.Context {
	return context.WithValue(ctx, uploadDispatchKey{}, uploadDispatch{ref: ref, authorize: authorize})
}

func UploadDispatchAuthorization(ctx context.Context, ref string) FileUploadAuthorization {
	dispatch, _ := ctx.Value(uploadDispatchKey{}).(uploadDispatch)
	if ref == "" || dispatch.ref != ref {
		return nil
	}
	return dispatch.authorize
}
