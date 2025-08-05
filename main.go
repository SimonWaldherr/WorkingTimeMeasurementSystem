package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/sessions"
)

// WorkHoursData is a struct that represents the data needed to display work hours
type WorkHoursData struct {
	UserName  string
	WorkDate  string
	WorkHours float64
}

// CurrentStatusData is a struct that represents the data needed to display the current status
type CurrentStatusData struct {
	UserName string
	Status   string
	Date     string
}

// AuthUser is a struct that represents a user in the CSV-based auth store
type AuthUser struct {
	Username string
	Password string
	Role     string
}

// BulkClockRequest represents the JSON payload for bulk clocking via barcode
type BulkClockRequest struct {
	ActivityCode string   `json:"activityCode"`
	UserCodes    []string `json:"userCodes"`
}

// loadCredentials loads the credentials from a CSV file
func loadCredentials(filename string) (map[string]AuthUser, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'
	reader.FieldsPerRecord = 3
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	users := make(map[string]AuthUser)
	for _, record := range records {
		users[record[0]] = AuthUser{
			Username: record[0],
			Password: record[1],
			Role:     record[2],
		}
	}
	return users, nil
}

var store *sessions.CookieStore

func initSessionStore() {
	config := getConfig()
	store = sessions.NewCookieStore([]byte(config.Security.SessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   config.Security.SessionDuration * 60, // Convert minutes to seconds
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
	}
}

func basicAuthMiddleware(users map[string]AuthUser, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		username, ok := session.Values["username"].(string)
		if !ok || username == "" {
			// not logged in: redirect to login page
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		_, exists := users[username]
		if !exists {
			// user not found in CSV: force logout
			session.Options.MaxAge = -1
			session.Save(r, w)
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		// Optionally: Session-Timeout erzwingen (optional, Cookie-Timeout reicht meist)
		next.ServeHTTP(w, r)
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func init() {
	// Initialize configuration
	initConfig()
	// Validate configuration
	if err := validateConfig(); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}
	// Initialize session store
	initSessionStore()
	// ensure schema is in place
	createDatabaseAndTables()
}

func main() {
	config := getConfig()
	
	// load auth users
	users, err := loadCredentials("credentials.csv")
	if err != nil {
		log.Fatalf("Error loading credentials: %v", err)
	}

	// Initialize service layer
	service := NewWorkingTimeService()
	defer service.Close()

	mux := http.NewServeMux()

	// Apply tenant middleware if multi-tenant is enabled
	var handler http.Handler = mux
	if config.Features.MultiTenant {
		handler = tenantMiddleware(mux)
	}

	// Login & Logout
	mux.Handle("/login", loginHandler(users))
	mux.HandleFunc("/logout", logoutHandler)

	// core pages (unprotected for demo, protected for multi-tenant)
	if config.Features.MultiTenant {
		mux.Handle("/", basicAuthMiddleware(users, http.HandlerFunc(indexHandler)))
	} else {
		mux.Handle("/", http.HandlerFunc(indexHandler))
	}
	
	mux.Handle("/addUser", basicAuthMiddleware(users, http.HandlerFunc(addUserHandler)))
	mux.Handle("/addActivity", basicAuthMiddleware(users, http.HandlerFunc(addActivityHandler)))
	mux.Handle("/addDepartment", basicAuthMiddleware(users, http.HandlerFunc(addDepartmentHandler)))
	mux.Handle("/clockInOutForm", http.HandlerFunc(clockInOutForm))
	mux.Handle("/current_status", http.HandlerFunc(currentStatusHandler))

	// protected actions
	mux.Handle("/createUser", basicAuthMiddleware(users, http.HandlerFunc(createUserHandler)))
	mux.Handle("/editUser", basicAuthMiddleware(users, http.HandlerFunc(editUserHandler)))
	mux.Handle("/createActivity", basicAuthMiddleware(users, http.HandlerFunc(createActivityHandler)))
	mux.Handle("/createDepartment", basicAuthMiddleware(users, http.HandlerFunc(createDepartmentHandler)))
	mux.Handle("/work_hours", basicAuthMiddleware(users, http.HandlerFunc(workHoursHandler)))
	mux.Handle("/work_status", basicAuthMiddleware(users, http.HandlerFunc(workStatusHandler)))

	// barcodes page (if enabled)
	if config.Features.BarcodeScanning {
		mux.Handle("/barcodes", basicAuthMiddleware(users, http.HandlerFunc(barcodesHandler)))
		mux.Handle("/scan", http.HandlerFunc(scanHandler))
		mux.Handle("/bulkClock", http.HandlerFunc(bulkClockHandler))
	}

	// static files (CSS, JS, images)
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// clock in/out via dropdown
	mux.Handle("/clockInOut", http.HandlerFunc(clockInOut))

	serverAddr := fmt.Sprintf("%s:%d", config.Server.Host, config.Server.Port)
	log.Printf("Starting server on %s…", serverAddr)
	log.Printf("Multi-tenant mode: %v", config.Features.MultiTenant)
	log.Printf("Database backend: %s", config.Database.Backend)
	
	server := &http.Server{
		Addr:           serverAddr,
		Handler:        handler,
		ReadTimeout:    time.Duration(config.Server.ReadTimeout) * time.Second,
		WriteTimeout:   time.Duration(config.Server.WriteTimeout) * time.Second,
		IdleTimeout:    time.Duration(config.Server.IdleTimeout) * time.Second,
		MaxHeaderBytes: config.Server.MaxHeaderBytes,
	}
	
	log.Fatal(server.ListenAndServe())
}

// indexHandler shows the home page
func indexHandler(w http.ResponseWriter, r *http.Request) {
	var users []User
	var activities []Activity
	
	config := getConfig()
	if config.Features.MultiTenant {
		tenantCtx, err := getTenantFromContext(r.Context())
		if err != nil {
			http.Error(w, "Tenant context required", http.StatusBadRequest)
			return
		}
		
		service := NewWorkingTimeService()
		defer service.Close()
		
		users, _ = service.GetUsersForTenant(r.Context(), tenantCtx.TenantID)
		activities, _ = service.GetActivitiesForTenant(r.Context(), tenantCtx.TenantID)
	} else {
		users = getUsers()
		activities = getActivities()
	}
	
	data := struct {
		Users      []User
		Activities []Activity
	}{
		Users:      users,
		Activities: activities,
	}
	renderTemplate(w, "index", data)
}

func loginHandler(users map[string]AuthUser) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			renderTemplate(w, "login", nil)
			return
		}
		// POST
		username := r.FormValue("username")
		password := r.FormValue("password")
		user, ok := users[username]
		if !ok || user.Password != password {
			renderTemplate(w, "login", map[string]interface{}{
				"Error": "Benutzername oder Passwort falsch.",
			})
			return
		}
		session, _ := store.Get(r, "session")
		session.Values["username"] = user.Username
		session.Options = &sessions.Options{
			Path:     "/",
			MaxAge:   sessionDuration * 60, // in Sekunden
			HttpOnly: true,
		}
		session.Save(r, w)
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	// Löscht die Session und leitet zur Login-Seite weiter
	session, _ := store.Get(r, "session")
	session.Options.MaxAge = -1 // Löscht das Cookie
	session.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// Entry-Struktur anpassen je nach deiner DB
type Entry struct {
	ID         int
	UserID     int
	UserName   string
	ActivityID string
	Date       string
	Start      string
	End        string
}

// Zeitformat: DD.MM.YYYY HH:MM[:SS]
const timeLayout = "02.01.2006 15:04:05" // oder ohne Sekunden "02.01.2006 15:04"

// clockInOutForm shows the manual clock in/out form
func clockInOutForm(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		users := getUsers()
		activities := getActivities()
		data := struct {
			Users      []User
			Activities []Activity
		}{
			Users:      users,
			Activities: activities,
		}
		renderTemplate(w, "clockInOutForm", data)
	}
}

// addUserHandler shows the add-user page
func addUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		depts := getDepartments()
		renderTemplate(w, "addUser", struct{ Departments []Department }{depts})
	}
}

// editUserHandler shows or processes the edit-user page
func editUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.FormValue("id")
		u := getUser(id)
		depts := getDepartments()
		renderTemplate(w, "editUser", struct {
			User        User
			Departments []Department
		}{u, depts})
		return
	} else if r.Method == http.MethodPost {
		id := r.FormValue("id")
		updateUser(id,
			r.FormValue("name"),
			r.FormValue("stampkey"),
			r.FormValue("email"),
			r.FormValue("position"),
			r.FormValue("department_id"),
		)
	}
	http.Redirect(w, r, "/addUser", http.StatusSeeOther)
}

// addActivityHandler shows the add-activity page
func addActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderTemplate(w, "addActivity", nil)
	}
}

// addDepartmentHandler shows the add-department page
func addDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderTemplate(w, "addDepartment", nil)
	}
}

// createUserHandler processes adding a new user
func createUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		createUser(
			r.FormValue("name"),
			r.FormValue("stampkey"),
			r.FormValue("email"),
			r.FormValue("position"),
			r.FormValue("department_id"),
		)
	}
	http.Redirect(w, r, "/addUser", http.StatusSeeOther)
}

func barcodesHandler(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Users      []User
		Activities []Activity
	}{
		Users:      getUsers(),
		Activities: getActivities(),
	}
	renderTemplate(w, "barcodes", data)
}

// createActivityHandler processes adding a new activity
func createActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		createActivity(
			r.FormValue("status"),
			r.FormValue("work"),
			r.FormValue("comment"),
		)
	}
	http.Redirect(w, r, "/addActivity", http.StatusSeeOther)
}

// createDepartmentHandler processes adding a new department
func createDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		createDepartment(r.FormValue("name"))
	}
	http.Redirect(w, r, "/addDepartment", http.StatusSeeOther)
}

// clockInOut handles manual clock in/out submissions
func clockInOut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	userID := r.FormValue("user_id")
	stampKey := r.FormValue("stampkey")
	activityID := r.FormValue("activity_id")
	if userID == "" && stampKey != "" {
		userID = getUserIDFromStampKey(stampKey)
	}
	if userID == "" || activityID == "" {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	createEntry(userID, activityID, time.Now())

	// Redirect back to the referring page
	http.Redirect(w, r, r.Header.Get("Referer"), http.StatusSeeOther)
}

type PageData struct {
	WorkHoursTable     TableData
	CurrentStatusTable TableData
}

func workStatusHandler(w http.ResponseWriter, r *http.Request) {
	workData := getWorkHoursData()
	statusData := getCurrentStatusData()

	workRows := make([][]interface{}, len(workData))
	for i, d := range workData {
		workRows[i] = []interface{}{d.UserName, d.WorkDate, d.WorkHours}
	}

	statusRows := make([][]interface{}, len(statusData))
	for i, d := range statusData {
		statusRows[i] = []interface{}{d.UserName, d.Status, d.Date}
	}

	pageData := PageData{
		WorkHoursTable: TableData{
			Headers: []string{"User Name", "Work Date", "Work Hours"},
			Rows:    workRows,
		},
		CurrentStatusTable: TableData{
			Headers: []string{"User Name", "Status", "Date"},
			Rows:    statusRows,
		},
	}

	tmpl := template.Must(template.New("page").ParseFiles("templates/layout.html")) // Oder dein Template-Setup
	if err := tmpl.ExecuteTemplate(w, "page", pageData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// workHoursHandler shows the work hours table
func workHoursHandler(w http.ResponseWriter, r *http.Request) {
	data := getWorkHoursData()
	headers := []string{"User Name", "Work Date", "Work Hours"}
	rows := make([][]interface{}, len(data))
	for i, d := range data {
		rows[i] = []interface{}{d.UserName, d.WorkDate, d.WorkHours}
	}
	renderHTMLTable(w, "Work Hours", TableData{Headers: headers, Rows: rows})
}

// currentStatusHandler shows who is currently clocked in/out
func currentStatusHandler(w http.ResponseWriter, r *http.Request) {
	data := getCurrentStatusData()
	headers := []string{"User Name", "Status", "Date"}
	rows := make([][]interface{}, len(data))
	for i, d := range data {
		rows[i] = []interface{}{d.UserName, d.Status, d.Date}
	}
	renderHTMLTable(w, "Current Status", TableData{Headers: headers, Rows: rows})
}

// scanHandler serves the barcode-scanning page
func scanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderTemplate(w, "scan", nil)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// bulkClockHandler processes the JSON payload from the scan page
func bulkClockHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req BulkClockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad payload", http.StatusBadRequest)
		return
	}

	db := getDB()
	defer db.Close()

	// look up activity by its code field (you must have added `code TEXT UNIQUE` to `type`)
	var activityID int
	if err := db.QueryRow("SELECT id FROM type WHERE code = ?", req.ActivityCode).Scan(&activityID); err != nil {
		http.Error(w, "Unknown activity code", http.StatusBadRequest)
		return
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare("INSERT INTO entries(date, type_id, user_id) VALUES (?, ?, ?)")
	defer stmt.Close()

	now := time.Now().Format(time.RFC3339)
	for _, code := range req.UserCodes {
		var userID int
		if err := db.QueryRow("SELECT id FROM users WHERE stampkey = ?", code).Scan(&userID); err != nil {
			// skip unknown cards
			continue
		}
		stmt.Exec(now, activityID, userID)
	}
	tx.Commit()
	w.WriteHeader(http.StatusNoContent)
}
