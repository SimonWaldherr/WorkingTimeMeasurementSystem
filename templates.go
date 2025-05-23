package main

import (
	"html/template"
	"net/http"
	"path"
)

var base *template.Template

func init() {
	// parse the layout + header + footer one time
	base = template.Must(
		template.ParseFiles(
			"templates/base.html",
			"templates/header.html",
			"templates/footer.html",
		),
	)
}

func renderTemplate(w http.ResponseWriter, page string, data interface{}) {
	// clone the base set so we can add exactly one page's content
	tmpl, err := base.Clone()
	if err != nil {
		http.Error(w, "template clone error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// parse the single page template (which only defines title+content)
	pageFile := path.Join("templates", page+".html")
	tmpl, err = tmpl.ParseFiles(pageFile)
	if err != nil {
		http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// execute the "base" template, which will pull in header, then override
	// the content block with whatever was defined in page+".html"
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "template execute error: "+err.Error(), http.StatusInternalServerError)
	}
}
