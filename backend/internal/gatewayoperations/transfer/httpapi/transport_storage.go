package filetransferhttp

import (
	"mime"
	"net/http"
	"path/filepath"

	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	"github.com/aipermission/aipermission/backend/internal/httpattachment"
)

func setDownloadHeaders(w http.ResponseWriter, fileName string) {
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	httpattachment.SetHeaders(w, fileName, contentType)
}

func cleanupTempPaths(paths []string) {
	filetransfer.CleanupPaths(paths)
}
