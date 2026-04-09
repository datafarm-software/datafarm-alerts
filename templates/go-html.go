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
	if opts.Files == nil {
		return nil, fmt.Errorf("nil files")
	}
	htmlPaths, err := findHtmlPaths(opts.Files)
	if err != nil {
		return nil, fmt.Errorf("finding htmlPaths: %v", err)
	}
	if len(htmlPaths) < 1 {
		return nil, fmt.Errorf("found no html files")
	}
	tmpl, err := parseTemplates(opts.Files, htmlPaths)
	if err != nil {
		return nil, fmt.Errorf("parseTemplates: %v", err)
	}
	return &Gohtml{tmpl}, nil
}

func (g *Gohtml) Execute(name string, pagedata any) (*strings.Builder, error) {
	sb := strings.Builder{}
	err := g.tmpl.ExecuteTemplate(&sb, name, pagedata)
	return &sb, err
}
