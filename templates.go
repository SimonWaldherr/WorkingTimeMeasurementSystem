package main

import (
    "bytes"
    "embed"
    "html/template"
    "net/http"
    "os"
    "path"
    "path/filepath"
    "strings"
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

// MetaInfo carries auth/role information for templates (header/nav)
type MetaInfo struct {
	IsAuthenticated bool
	IsAdmin         bool
	Username        string
	Title           string
}

// ViewModel wraps page content with meta info; base.html passes .Content as dot to blocks
type ViewModel struct {
	Meta    MetaInfo
	Content interface{}
}

func buildMeta(r *http.Request, title string) MetaInfo {
	meta := MetaInfo{Title: title}
	if r == nil {
		return meta
	}
	session, _ := store.Get(r, "session")
	if uname, ok := session.Values["username"].(string); ok && uname != "" {
		meta.IsAuthenticated = true
		meta.Username = uname
	}
	if role, ok := session.Values["role"].(string); ok && role != "" {
		// treat case-insensitively
		if role == "admin" || role == "Admin" || role == "ADMIN" {
			meta.IsAdmin = true
		}
	}
	return meta
}

func renderTemplate(w http.ResponseWriter, r *http.Request, page string, data interface{}) {
    // Clone base to avoid polluting it
    tmpl, err := base.Clone()
    if err != nil {
        http.Error(w, "template clone error: "+err.Error(), http.StatusInternalServerError)
        return
    }

    pageFile := path.Join("templates", page+".html")

    // Tenant-aware overrides: tenant/<host>/templates/{base,header,footer,page}.html
    var safeHost string
    if r != nil {
        host := r.Host
        if idx := strings.IndexByte(host, ':'); idx >= 0 { host = host[:idx] }
        safeHost = strings.ToLower(strings.ReplaceAll(host, "/", "-"))
    }

    // Helper to test file existence
    exists := func(p string) bool { if info, err := os.Stat(p); err == nil && !info.IsDir() { return true }; return false }
    // Parse tenant overrides for base/header/footer if present
    if safeHost != "" {
        for _, name := range []string{"base", "header", "footer"} {
            tf := filepath.Join("tenant", safeHost, "templates", name+".html")
            if exists(tf) {
                if _, err := tmpl.ParseFiles(tf); err != nil {
                    http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
                    return
                }
            }
        }
    }

	// Check if base was loaded from disk or embed by checking the type of base (optional)
	// or just attempt to parse from disk first if folder exists,
	// else parse from embedded FS

    if safeHost != "" {
        tenantPage := filepath.Join("tenant", safeHost, "templates", page+".html")
        if exists(tenantPage) {
            tmpl, err = tmpl.ParseFiles(tenantPage)
        } else if info, statErr := os.Stat("templates"); statErr == nil && info.IsDir() {
            tmpl, err = tmpl.ParseFiles(pageFile)
        } else {
            tmpl, err = tmpl.ParseFS(templatesFS, pageFile)
        }
    } else {
        if info, statErr := os.Stat("templates"); statErr == nil && info.IsDir() {
            tmpl, err = tmpl.ParseFiles(pageFile)
        } else {
            tmpl, err = tmpl.ParseFS(templatesFS, pageFile)
        }
    }

	if err != nil {
		http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure Content is never nil to avoid nil deref in templates (e.g., .Content.Error)
	var content interface{} = data
	if content == nil {
		content = map[string]interface{}{}
	}
	vm := ViewModel{Meta: buildMeta(r, ""), Content: content}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", vm); err != nil {
		http.Error(w, "template execute error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

// TableData is used to render a generic HTML table
type TableData struct {
	Title   string
	Headers []string
	Rows    [][]interface{}
}

// renderHTMLTable renders a simple HTML table
func renderHTMLTable(w http.ResponseWriter, r *http.Request, title string, td TableData) {
    // Clone base to avoid polluting it
    tmpl, err := base.Clone()
    if err != nil {
        http.Error(w, "template clone error: "+err.Error(), http.StatusInternalServerError)
        return
    }

    pageFile := path.Join("templates", "table.html")
    // Tenant-aware overrides similar to renderTemplate
    var safeHost string
    if r != nil {
        host := r.Host
        if idx := strings.IndexByte(host, ':'); idx >= 0 { host = host[:idx] }
        safeHost = strings.ToLower(strings.ReplaceAll(host, "/", "-"))
    }
    exists := func(p string) bool { if info, err := os.Stat(p); err == nil && !info.IsDir() { return true }; return false }
    if safeHost != "" {
        for _, name := range []string{"base", "header", "footer"} {
            tf := filepath.Join("tenant", safeHost, "templates", name+".html")
            if exists(tf) { tmpl, err = tmpl.ParseFiles(tf); if err != nil { http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError); return } }
        }
        tenantPage := filepath.Join("tenant", safeHost, "templates", "table.html")
        if exists(tenantPage) {
            tmpl, err = tmpl.ParseFiles(tenantPage)
        } else if info, statErr := os.Stat("templates"); statErr == nil && info.IsDir() {
            tmpl, err = tmpl.ParseFiles(pageFile)
        } else {
            tmpl, err = tmpl.ParseFS(templatesFS, pageFile)
        }
    } else {
        if info, statErr := os.Stat("templates"); statErr == nil && info.IsDir() {
            tmpl, err = tmpl.ParseFiles(pageFile)
        } else {
            tmpl, err = tmpl.ParseFS(templatesFS, pageFile)
        }
    }

	if err != nil {
		http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Ensure title is available to base/meta
	vm := ViewModel{Meta: buildMeta(r, title), Content: td}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", vm); err != nil {
		http.Error(w, "template execute error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func renderError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	vm := ViewModel{Meta: MetaInfo{Title: "Error"}, Content: map[string]interface{}{
		"Status":  status,
		"Message": message,
	}}
	if err := base.ExecuteTemplate(w, "base", vm); err != nil {
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
