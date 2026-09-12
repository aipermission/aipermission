package gatewayworkspace

import operationcapability "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/capabilities/operation"

func (runtime *Runtime) CommandCapability() (operationcapability.CommandCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return operationcapability.CommandCapability{}, false
	}
	return operationcapability.NewCommand(
		owner.Storage.DatabaseHandle(), owner.Storage.SecretVault(), owner.Connectors.ConsoleSessionManager(),
	), true
}

func (runtime *Runtime) CommandBulkCapability() (operationcapability.CommandBulkCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return operationcapability.CommandBulkCapability{}, false
	}
	return operationcapability.NewCommandBulk(owner.Connectors.ConsoleSessionManager()), true
}

func (runtime *Runtime) LiveConsoleCapability() (operationcapability.LiveConsoleCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return operationcapability.LiveConsoleCapability{}, false
	}
	return operationcapability.NewLiveConsole(owner.Connectors.ConsoleSessionManager()), true
}

func (runtime *Runtime) BackupCapability() (operationcapability.BackupCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return operationcapability.BackupCapability{}, false
	}
	return operationcapability.NewBackup(owner.Storage.DatabaseHandle(), owner.Storage.SecretVault()), true
}

func (runtime *Runtime) PasswordValidationCapability() (operationcapability.PasswordValidationCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return operationcapability.PasswordValidationCapability{}, false
	}
	return operationcapability.NewPasswordValidation(owner.Storage.DatabaseHandle()), true
}

func (runtime *Runtime) TransferCapability() (operationcapability.TransferCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return operationcapability.TransferCapability{}, false
	}
	return operationcapability.NewTransfer(owner.Storage.DatabaseHandle()), true
}

func (runtime *Runtime) PeerTrustCapability() (operationcapability.PeerTrustCapability, bool) {
	owner, ok := runtime.concreteOwner()
	if !ok {
		return operationcapability.PeerTrustCapability{}, false
	}
	return operationcapability.NewPeerTrust(owner.Security.VaultDeliveryCoordinator()), true
}
