package main

import (
	"embed"
	"html/template"
	"net/http"
	"os"
	"path"
)

//go:embed templates/*.html
var templatesFS embed.FS

var base *template.Template

func init() {
	const templatesDir = "templates"

	// Check if templates folder exists on disk
	if info, err := os.Stat(templatesDir); err == nil && info.IsDir() {
		// Folder exists, parse templates from disk
		var err error
		base, err = template.ParseFiles(
			path.Join(templatesDir, "base.html"),
			path.Join(templatesDir, "header.html"),
			path.Join(templatesDir, "footer.html"),
		)
		if err != nil {
			panic("failed to parse templates from disk: " + err.Error())
		}
	} else {
		// Folder does not exist, parse templates from embedded FS
		var err error
		base, err = template.ParseFS(templatesFS, "templates/base.html", "templates/header.html", "templates/footer.html")
		if err != nil {
			panic("failed to parse embedded templates: " + err.Error())
		}
	}
}

func renderTemplate(w http.ResponseWriter, page string, data interface{}) {
	// Clone base to avoid polluting it
	tmpl, err := base.Clone()
	if err != nil {
		http.Error(w, "template clone error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pageFile := path.Join("templates", page+".html")

	// Check if base was loaded from disk or embed by checking the type of base (optional)
	// or just attempt to parse from disk first if folder exists,
	// else parse from embedded FS

	if info, err := os.Stat("templates"); err == nil && info.IsDir() {
		// Parse page template from disk
		tmpl, err = tmpl.ParseFiles(pageFile)
	} else {
		// Parse page template from embedded FS
		tmpl, err = tmpl.ParseFS(templatesFS, pageFile)
	}

	if err != nil {
		http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "template execute error: "+err.Error(), http.StatusInternalServerError)
	}
}

// TableData is used to render a generic HTML table
type TableData struct {
	Title   string
	Headers []string
	Rows    [][]interface{}
}

// renderHTMLTable renders a simple HTML table
func renderHTMLTable(w http.ResponseWriter, title string, td TableData) {
	// Clone base to avoid polluting it
	tmpl, err := base.Clone()
	if err != nil {
		http.Error(w, "template clone error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pageFile := path.Join("templates", "table.html")
	// Check if base was loaded from disk or embed by checking the type of base (optional)
	if info, err := os.Stat("templates"); err == nil && info.IsDir() {
		// Parse page template from disk

		tmpl, err = tmpl.ParseFiles(pageFile)
	} else {
		// Parse page template from embedded FS
		tmpl, err = tmpl.ParseFS(templatesFS, pageFile)
	}

	if err != nil {
		http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = tmpl.ExecuteTemplate(w, "base", struct {
		Title string
		Table TableData
	}{title, td})
	if err != nil {
		http.Error(w, "template execute error: "+err.Error(), http.StatusInternalServerError)
	}
}

func renderError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	data := map[string]interface{}{
		"Status":  status,
		"Message": message,
	}
	if err := base.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "template execute error: "+err.Error(), http.StatusInternalServerError)
	}
}
func renderNotFound(w http.ResponseWriter) {
	renderError(w, http.StatusNotFound, "Page not found")
}
func renderInternalServerError(w http.ResponseWriter, err error) {
	renderError(w, http.StatusInternalServerError, "Internal server error: "+err.Error())
}
func renderBadRequest(w http.ResponseWriter, err error) {
	renderError(w, http.StatusBadRequest, "Bad request: "+err.Error())
}
func renderUnauthorized(w http.ResponseWriter, err error) {
	renderError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error())
}
func renderForbidden(w http.ResponseWriter, err error) {
	renderError(w, http.StatusForbidden, "Forbidden: "+err.Error())
}
func renderServiceUnavailable(w http.ResponseWriter, err error) {
	renderError(w, http.StatusServiceUnavailable, "Service unavailable: "+err.Error())
}
func renderTooManyRequests(w http.ResponseWriter, err error) {
	renderError(w, http.StatusTooManyRequests, "Too many requests: "+err.Error())
}
