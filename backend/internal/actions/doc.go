// Package actions contains the generic connector action service skeleton.
//
// It is deliberately small and not wired into the current SSH runtime yet. The
// package establishes the future flow from a target reference to a connector
// prepared action without making target IDs ambiguous between legacy SSH server
// IDs and connector targets.
package actions
