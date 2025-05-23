package main

import (
	//"context"
	//"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"html/template"
	//"database/sql"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	//"strings"
	"time"

	"github.com/gorilla/sessions"
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

var store = sessions.NewCookieStore([]byte("change-me-very-secret"))

// Session duration in minutes
const sessionDuration = 30

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
	// ensure schema is in place
	createDatabaseAndTables()
}

func main() {
	// load auth users
	users, err := loadCredentials("credentials.csv")
	if err != nil {
		log.Fatalf("Error loading credentials: %v", err)
	}

	mux := http.NewServeMux()

	// Login & Logout
	mux.Handle("/login", loginHandler(users))
	mux.HandleFunc("/logout", logoutHandler)

	// core pages (unprotected)
	mux.Handle("/", http.HandlerFunc(indexHandler))
	mux.Handle("/addUser", basicAuthMiddleware(users, http.HandlerFunc(addUserHandler)))
	mux.Handle("/addActivity", basicAuthMiddleware(users, http.HandlerFunc(addActivityHandler)))
	mux.Handle("/addDepartment", basicAuthMiddleware(users, http.HandlerFunc(addDepartmentHandler)))
	mux.Handle("/clockInOutForm", http.HandlerFunc(clockInOutForm))

	// protected actions
	mux.Handle("/createUser", basicAuthMiddleware(users, http.HandlerFunc(createUserHandler)))
	mux.Handle("/editUser", basicAuthMiddleware(users, http.HandlerFunc(editUserHandler)))
	mux.Handle("/createActivity", basicAuthMiddleware(users, http.HandlerFunc(createActivityHandler)))
	mux.Handle("/createDepartment", basicAuthMiddleware(users, http.HandlerFunc(createDepartmentHandler)))
	mux.Handle("/work_hours", basicAuthMiddleware(users, http.HandlerFunc(workHoursHandler)))
	mux.Handle("/current_status", basicAuthMiddleware(users, http.HandlerFunc(currentStatusHandler)))
	mux.Handle("/entries_view", basicAuthMiddleware(users, http.HandlerFunc(entriesViewHandler)))

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

func entriesViewHandler(w http.ResponseWriter, r *http.Request) {
	// Zeitformat wie gewohnt
	const timeLayout = "02.01.2006 15:04:05"

	if r.Method == "POST" {
		r.ParseForm()
		user := r.Form.Get("user")
		activity := r.Form.Get("activity")
		start := r.Form.Get("start")
		end := r.Form.Get("end")
		id := r.Form.Get("id") // für Edit

		// Zeit parsen
		startTime, err := time.Parse(timeLayout, start)
		if err != nil {
			http.Error(w, "Startzeit ungültig", 400)
			return
		}
		endTime, err := time.Parse(timeLayout, end)
		if err != nil {
			http.Error(w, "Endzeit ungültig", 400)
			return
		}

		db := getDB()
		defer db.Close()
		var dbErr error
		if id == "" {
			// INSERT
			_, dbErr = db.Exec("INSERT INTO entries (user, activity, start, end) VALUES (?, ?, ?, ?)", user, activity, startTime, endTime)
		} else {
			// UPDATE
			_, dbErr = db.Exec("UPDATE entries SET user=?, activity=?, start=?, end=? WHERE id=?", user, activity, startTime, endTime, id)
		}
		if dbErr != nil {
			http.Error(w, "DB Fehler", 500)
			return
		}
		http.Redirect(w, r, "/entries_view", http.StatusSeeOther)
		return
	}

	// GET: Seite anzeigen
	entries := getEntries()
	users := getUsers()
	activities := getActivities()

	tmpl, err := template.ParseFiles("templates/entries_view.html")
	if err != nil {
		http.Error(w, "Template-Fehler", 500)
		return
	}
	tmpl.Execute(w, map[string]interface{}{
		"Entries":    entries,
		"Users":      users,
		"Activities": activities,
		"TimeLayout": timeLayout[:16], // Für das Input-Feld (ohne Sekunden)
	})
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
