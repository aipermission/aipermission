// Package gatewayconnectormanagement is the connector catalog and target-management contract consumed by the gateway.
package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"errors"
	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"net/http"
)

func InvalidTargetRefError() error { return connectortargets.ErrInvalidTargetRef }

func IsInvalidTargetRef(err error) bool { return errors.Is(err, connectortargets.ErrInvalidTargetRef) }
func IsRuntimeSurfaceNotFound(err error) bool {
	return errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound)
}
func IsTargetNotFound(err error) bool { return errors.Is(err, connectortargets.ErrTargetNotFound) }
func IsTargetProfileNotFound(err error) bool {
	return errors.Is(err, connectortargets.ErrTargetProfileNotFound)
}

type AuditAppender connectormanagement.AuditAppender
type ConnectionTestResponse connectormanagement.ConnectionTestResponse
type CreateTargetRequest connectormanagement.CreateTargetRequest
type CreateTargetWithProfileRequest struct {
	Target  CreateTargetRequest    `json:"target"`
	Profile CredentialProfileInput `json:"profile"`
}
type CredentialCanonicalizer connectormanagement.CredentialCanonicalizer
type CredentialStorage connectormanagement.CredentialStorage
type CredentialProfileInput connectormanagement.CredentialProfileInput
type PreparedCredentialProfile connectormanagement.PreparedCredentialProfile
type ProfileSummary connectormanagement.ProfileSummary
type ProvisionRequest connectormanagement.ProvisionRequest
type TargetLifecycleChange connectormanagement.TargetLifecycleChange
type TargetResponse connectormanagement.TargetResponse
type UpdateTargetRequest connectormanagement.UpdateTargetRequest
type UpdateTargetWithProfileRequest struct {
	Target  UpdateTargetRequest    `json:"target"`
	Profile CredentialProfileInput `json:"profile"`
}
type ConnectorApprovalItem connectorapproval.Item
type ConnectorApprovalNoteRequest connectorapproval.NoteRequest

type ConnectorApprovalWorkflow interface {
	ApprovalPreview(context.Context, ActionRequest) (map[string]any, error)
	RunPending(context.Context, int64, string) (ActionRequest, error)
	DeclinePending(context.Context, int64, string) (ActionRequest, error)
}

type ConnectorApprovalRequestStore interface {
	ListActionRequests(context.Context, connectortargets.ActionRequestFilter) ([]connectortargets.ActionRequest, error)
	GetActionRequest(context.Context, int64) (connectortargets.ActionRequest, error)
}

type connectorApprovalRequestReader struct{ store *connectortargets.Store }

func NewConnectorApprovalRequestStore(database *sql.DB) ConnectorApprovalRequestStore {
	return connectorApprovalRequestReader{store: connectortargets.NewStore(database)}
}

func (reader connectorApprovalRequestReader) ListActionRequests(ctx context.Context, filter connectortargets.ActionRequestFilter) ([]connectortargets.ActionRequest, error) {
	return reader.store.ListActionRequests(ctx, filter)
}

func (reader connectorApprovalRequestReader) GetActionRequest(ctx context.Context, id int64) (connectortargets.ActionRequest, error) {
	return reader.store.GetActionRequest(ctx, id)
}

type ConnectorApprovalScope struct {
	Requests   ConnectorApprovalRequestStore
	Workflow   func() (ConnectorApprovalWorkflow, error)
	MCPStarted func() bool
	Redact     func(context.Context, string) string
}

type ConnectorApprovalScopeProvider func(http.ResponseWriter) (ConnectorApprovalScope, bool)

type CredentialBoundary struct {
	value connectormanagement.CredentialBoundary
}

func wrapCredentialBoundary(value connectormanagement.CredentialBoundary) CredentialBoundary {
	return CredentialBoundary{value: value}
}

func (boundary CredentialBoundary) domain() connectormanagement.CredentialBoundary {
	return boundary.value
}
func (boundary CredentialBoundary) Add(values ...string)       { boundary.value.Add(values...) }
func (boundary CredentialBoundary) AddStructured(value any)    { boundary.value.AddStructured(value) }
func (boundary CredentialBoundary) Empty() bool                { return boundary.value.Empty() }
func (boundary CredentialBoundary) Redact(value string) string { return boundary.value.Redact(value) }
func (boundary CredentialBoundary) RedactKey(value string) string {
	return boundary.value.RedactKey(value)
}
func (boundary CredentialBoundary) RedactStructured(value any) any {
	return boundary.value.RedactStructured(value)
}
func (boundary CredentialBoundary) Valid() bool { return boundary.value.Valid() }

type CredentialPreparationPorts struct {
	value connectormanagement.CredentialPreparationPorts
}

func (ports CredentialPreparationPorts) domain() connectormanagement.CredentialPreparationPorts {
	return ports.value
}
func (ports CredentialPreparationPorts) Encrypt(ctx context.Context, profileID int64, secret map[string]any) (string, error) {
	if ports.value.Encrypt == nil {
		return "", errors.New("credential secret encryption is unavailable")
	}
	return ports.value.Encrypt(ctx, profileID, secret)
}

type CredentialRuntimePorts struct {
	value connectormanagement.CredentialRuntimePorts
}

func (ports CredentialRuntimePorts) domain() connectormanagement.CredentialRuntimePorts {
	return ports.value
}
