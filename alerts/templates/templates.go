package templates

import (
	"fmt"
	"html/template"
	"io/fs"
	"path/filepath"

	"github.com/datafarm-software/datafarm-alerts/alerts/store"
)

type TemplateName string

const AlertNotificationTempl TemplateName = "alert-notification"
const AlertCreatedTempl TemplateName = "alert-create"
const AlertDeletedTempl TemplateName = "alert-delete"
const AlertUpdatedTempl TemplateName = "alert-update"

type Templater interface {
	Render(name TemplateName, data any) (string, error)
}

func findHtmlPaths(files fs.FS) ([]string, error) {
	var htmlTemplatePaths []string
	if err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, recursiveErr error) error {
		if recursiveErr != nil {
			return recursiveErr
		}
		if !d.IsDir() && filepath.Ext(path) == ".html" {
			htmlTemplatePaths = append(htmlTemplatePaths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("error walking through template files: %v", err)
	}
	if len(htmlTemplatePaths) < 1 {
		return nil, fmt.Errorf("found no templates in the filesystem")
	}
	return htmlTemplatePaths, nil
}

func parseTemplates(files fs.FS, htmlPaths []string) (*template.Template, error) {
	funcMap := map[string]any{
		"alertTypeToString":  alertstore.AlertTypeToString,
		"alertLevelToString": alertstore.AlertLevelToString,
	}
	return template.New("base").Funcs(funcMap).ParseFS(files, htmlPaths...)
}
