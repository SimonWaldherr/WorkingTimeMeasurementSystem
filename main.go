package main

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
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

// basicAuthMiddleware is a middleware that checks for basic auth credentials
func basicAuthMiddleware(users map[string]AuthUser, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			unauthorized(w)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "basic" {
			unauthorized(w)
			return
		}

		payload, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			unauthorized(w)
			return
		}

		pair := strings.SplitN(string(payload), ":", 2)
		if len(pair) != 2 {
			unauthorized(w)
			return
		}

		user, ok := users[pair[0]]
		if !ok || user.Password != pair[1] {
			unauthorized(w)
			return
		}

		// add role to context if needed
		ctx := context.WithValue(r.Context(), "role", user.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func init() {
	// ensure schema is in place
	createDatabaseAndTables()
}

func main() {
	// load basic-auth users
	users, err := loadCredentials("credentials.csv")
	if err != nil {
		log.Fatalf("Error loading credentials: %v", err)
	}

	mux := http.NewServeMux()

	// core pages (unprotected)
	mux.Handle("/", http.HandlerFunc(indexHandler))
	mux.Handle("/addUser", http.HandlerFunc(addUserHandler))
	mux.Handle("/addActivity", http.HandlerFunc(addActivityHandler))
	mux.Handle("/addDepartment", http.HandlerFunc(addDepartmentHandler))
	mux.Handle("/clockInOutForm", http.HandlerFunc(clockInOutForm))

	// protected actions
	mux.Handle("/createUser", basicAuthMiddleware(users, http.HandlerFunc(createUserHandler)))
	mux.Handle("/editUser", basicAuthMiddleware(users, http.HandlerFunc(editUserHandler)))
	mux.Handle("/createActivity", basicAuthMiddleware(users, http.HandlerFunc(createActivityHandler)))
	mux.Handle("/createDepartment", basicAuthMiddleware(users, http.HandlerFunc(createDepartmentHandler)))
	mux.Handle("/work_hours", basicAuthMiddleware(users, http.HandlerFunc(workHoursHandler)))
	mux.Handle("/current_status", basicAuthMiddleware(users, http.HandlerFunc(currentStatusHandler)))

	// clock in/out via dropdown
	mux.Handle("/clockInOut", http.HandlerFunc(clockInOut))

	// barcode-driven bulk clock
	mux.Handle("/scan", http.HandlerFunc(scanHandler))
	mux.Handle("/bulkClock", http.HandlerFunc(bulkClockHandler))

	log.Printf("Starting server on :8080…")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// indexHandler shows the home page
func indexHandler(w http.ResponseWriter, r *http.Request) {
	users := getUsers()
	activities := getActivities()
	data := struct {
		Users      []User
		Activities []Activity
	}{
		Users:      users,
		Activities: activities,
	}
	renderTemplate(w, "index", data)
}

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
	db := getDB()
	defer db.Close()
	stmt, err := db.Prepare("INSERT INTO entries (date, type_id, user_id) VALUES (?, ?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	now := time.Now().Format(time.RFC3339)
	uid, _ := strconv.Atoi(userID)
	aid, _ := strconv.Atoi(activityID)
	if _, err := stmt.Exec(now, aid, uid); err != nil {
		log.Fatal(err)
	}

	http.Redirect(w, r, r.Header.Get("Referer"), http.StatusSeeOther)
}

// workHoursHandler shows the work hours table
func workHoursHandler(w http.ResponseWriter, r *http.Request) {
	data := getWorkHoursData()
	headers := []string{"User Name", "Work Date", "Work Hours"}
	rows := make([][]interface{}, len(data))
	for i, d := range data {
		rows[i] = []interface{}{d.UserName, d.WorkDate, d.WorkHours}
	}
	renderHTMLTable(w, TableData{Headers: headers, Rows: rows})
}

// currentStatusHandler shows who is currently clocked in/out
func currentStatusHandler(w http.ResponseWriter, r *http.Request) {
	data := getCurrentStatusData()
	headers := []string{"User Name", "Status", "Date"}
	rows := make([][]interface{}, len(data))
	for i, d := range data {
		rows[i] = []interface{}{d.UserName, d.Status, d.Date}
	}
	renderHTMLTable(w, TableData{Headers: headers, Rows: rows})
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

// TableData is used to render a generic HTML table
type TableData struct {
	Headers []string
	Rows    [][]interface{}
}

// renderHTMLTable renders a simple HTML table
func renderHTMLTable(w io.Writer, td TableData) {
	const tmpl = `
<table class="table table-striped">
  <thead>
    <tr>{{- range .Headers }}<th>{{ . }}</th>{{- end }}</tr>
  </thead>
  <tbody>
    {{- range .Rows }}
      <tr>{{- range . }}<td>{{ . }}</td>{{- end }}</tr>
    {{- end }}
  </tbody>
</table>`
	t := template.Must(template.New("table").Parse(tmpl))
	if err := t.Execute(w, td); err != nil {
		log.Printf("Error rendering table: %v", err)
	}
}
