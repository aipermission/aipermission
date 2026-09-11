package api

type unlockRequest struct {
	Password   string `json:"password"`
	DatabaseID string `json:"database_id"`
}

type setupUnlockRequest struct {
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	DatabaseID      string `json:"database_id"`
	DatabaseName    string `json:"database_name"`
}

type renameDatabaseRequest struct {
	DatabaseName    string `json:"database_name"`
	CurrentPassword string `json:"current_password"`
}

type deleteDatabaseRequest struct {
	ConfirmName     string `json:"confirm_name"`
	CurrentPassword string `json:"current_password"`
}

type deleteLockedDatabaseRequest struct {
	DatabaseID      string `json:"database_id"`
	CurrentPassword string `json:"current_password"`
}

type switchDatabaseRequest struct {
	DatabaseID string `json:"database_id"`
	Password   string `json:"password"`
}

type changeDatabasePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}
