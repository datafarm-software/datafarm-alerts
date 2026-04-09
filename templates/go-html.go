package templates

import (
	"fmt"
	"html/template"
	"io/fs"
	"strings"
)

type GohtmlOpts struct {
	Files fs.FS
}

type Gohtml struct {
	tmpl *template.Template
}

func NewGohtml(opts GohtmlOpts) (Templater, error) {
	htmlPaths, err := findHtmlPaths(opts.Files)
	if err != nil {
		return nil, fmt.Errorf("finding htmlPaths: %v", err)
	}
	tmpl, err := parseTemplates(opts.Files, htmlPaths)
	return &Gohtml{tmpl}, nil
}

func (g *Gohtml) Execute(name string, pagedata any) (*strings.Builder, error) {
	sb := strings.Builder{}
	err := g.tmpl.ExecuteTemplate(&sb, name, pagedata)
	return &sb, err
}
