// Package credentialresource owns transport-neutral, connector-scoped resource
// contracts shared by core connectors and gateway adapters. It has no gateway
// runtime or persistence dependencies.
package credentialresource

import "context"

// CredentialResource describes one connector-owned encrypted resource without
// exposing its secret payload.
type CredentialResource struct {
	ID           int64
	Name         string
	ResourceType string
	PublicData   string
	Fingerprint  string
	CreatedAt    string
	UpdatedAt    string
}

type CreateCredentialResourceInput struct {
	Name         string
	ResourceType string
	PublicData   string
	Fingerprint  string
	Secret       any
}

type UpdateCredentialResourceInput struct {
	Name       string
	PublicData string
}

// CredentialResourceStore is scoped by core to one connector and resource
// kind. It can never query arbitrary tables or decrypt another resource class.
type CredentialResourceStore interface {
	List(ctx context.Context) ([]CredentialResource, error)
	Get(ctx context.Context, id int64) (CredentialResource, error)
	GetSecret(ctx context.Context, id int64, destination any) error
	Create(ctx context.Context, input CreateCredentialResourceInput) (CredentialResource, error)
	Update(ctx context.Context, id int64, input UpdateCredentialResourceInput) (CredentialResource, error)
	Delete(ctx context.Context, id int64) error
	CountProfileReferences(ctx context.Context, publicField string, numericValue int64) (int, error)
}

// CredentialResourceReader exposes only public evidence. Unlike the mutable
// store it cannot decrypt, create, update, delete or enumerate credentials.
type CredentialResourceReader interface {
	Get(context.Context, int64) (CredentialResource, error)
}

// ScopedResourceRuntime exposes persistent resources for one core-bound
// connector kind, without target resolution, console or principal authority.
type ScopedResourceRuntime interface {
	CredentialResources(resourceKind string) CredentialResourceStore
}

// EvidenceResourceRuntime is scoped to a connector by core. Its concrete
// readers, not merely their static interfaces, must exclude mutable methods.
type EvidenceResourceRuntime interface {
	CredentialResources(resourceKind string) CredentialResourceReader
}
