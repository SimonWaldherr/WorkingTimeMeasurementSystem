package main

import (
	//"context"
	//"encoding/base64"
	"encoding/csv"
	"encoding/json"

	//"database/sql"

    "log"
    "net/http"
    "os"

    "strings"
    "time"
    "strconv"
    "golang.org/x/crypto/bcrypt"

    "github.com/gorilla/sessions"
    "path/filepath"
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


func init() {
	// ensure schema is in place
	createDatabaseAndTables()
}

func main() {
	// load auth users
	log.Printf("Starting WorkingTime with %s…", dbBackend)
	log.Printf("  DB_BACKEND = %s", dbBackend)
	if dbBackend == "sqlite" {
		log.Printf("  SQLITE_PATH = %s", os.Getenv("SQLITE_PATH"))
	} else {
		log.Printf("  MSSQL_SERVER = %s", os.Getenv("MSSQL_SERVER"))
		log.Printf("  MSSQL_DATABASE = %s", os.Getenv("MSSQL_DATABASE"))
		log.Printf("  MSSQL_USER = %s", os.Getenv("MSSQL_USER"))
	}
	users, err := loadCredentials("credentials.csv")
	if err != nil {
		log.Fatalf("Error loading credentials: %v", err)
	}
	log.Printf("  Credentials file = %s", "credentials.csv")

    mux := http.NewServeMux()

	// Login & Logout
	mux.Handle("/login", loginHandler(users))
	mux.HandleFunc("/logout", logoutHandler)
	// Password-based stamping page
	mux.HandleFunc("/passwordStamp", passwordStampHandler)

	// core pages (unprotected)
	mux.Handle("/", basicAuthMiddleware(users, http.HandlerFunc(indexHandler)))
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
	//mux.Handle("/entries_view", basicAuthMiddleware(users, http.HandlerFunc(entriesViewHandler)))

    // Enhanced statistics and management
    mux.Handle("/dashboard", basicAuthMiddleware(users, http.HandlerFunc(dashboardHandler)))
    mux.Handle("/entries", basicAuthMiddleware(users, http.HandlerFunc(entriesHandler)))
	mux.Handle("/editEntry", basicAuthMiddleware(users, http.HandlerFunc(editEntryHandler)))
	mux.Handle("/editActivity", basicAuthMiddleware(users, http.HandlerFunc(editActivityHandler)))
	mux.Handle("/editDepartment", basicAuthMiddleware(users, http.HandlerFunc(editDepartmentHandler)))
	mux.Handle("/deleteEntry", basicAuthMiddleware(users, http.HandlerFunc(deleteEntryHandler)))
	mux.Handle("/deleteActivity", basicAuthMiddleware(users, http.HandlerFunc(deleteActivityHandler)))
	mux.Handle("/deleteDepartment", basicAuthMiddleware(users, http.HandlerFunc(deleteDepartmentHandler)))
	mux.Handle("/deleteUser", basicAuthMiddleware(users, http.HandlerFunc(deleteUserHandler)))

    // barcodes page
    mux.Handle("/barcodes", basicAuthMiddleware(users, http.HandlerFunc(barcodesHandler)))

    // Admin downloads (CSV)
    mux.Handle("/admin/download/entries.csv", adminOnly(http.HandlerFunc(downloadEntriesCSV)))
    mux.Handle("/admin/download/work_hours.csv", adminOnly(http.HandlerFunc(downloadWorkHoursCSV)))

    // User self history (no session required; verifies by email+password per request)
    mux.HandleFunc("/myHistory", myHistoryHandler)

    // static files (CSS, JS, images) with tenant override
    defaultStatic := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
    mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
        rel := strings.TrimPrefix(r.URL.Path, "/static/")
        host := r.Host
        if idx := strings.IndexByte(host, ':'); idx >= 0 { host = host[:idx] }
        safe := strings.ToLower(strings.ReplaceAll(host, "/", "-"))
        tenantPath := filepath.Join("tenant", safe, "static", rel)
        if info, err := os.Stat(tenantPath); err == nil && !info.IsDir() {
            http.ServeFile(w, r, tenantPath)
            return
        }
        defaultStatic.ServeHTTP(w, r)
    })

	// clock in/out via dropdown
	mux.Handle("/clockInOut", http.HandlerFunc(clockInOut))

    // barcode-driven bulk clock
    mux.Handle("/scan", http.HandlerFunc(scanHandler))
    mux.Handle("/bulkClock", http.HandlerFunc(bulkClockHandler))

	log.Printf("App will listen on http://localhost:8083")
	log.Printf("Starting server on :8083…")
	// Root wrapper to bind request host for multi-tenant SQLite
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if idx := strings.IndexByte(host, ':'); idx >= 0 { // strip port
			host = host[:idx]
		}
		SetRequestHost(host)
		// ensure per-host SQLite DB has schema
		EnsureSchemaCurrent()
		defer ClearRequestHost()
		mux.ServeHTTP(w, r)
	})
	log.Fatal(http.ListenAndServe(":8083", root))
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
	renderTemplate(w, r, "index", data)
}

func loginHandler(users map[string]AuthUser) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method == http.MethodGet {
            renderTemplate(w, r, "login", nil)
            return
        }
        // POST
        username := r.FormValue("username")
        password := r.FormValue("password")
        user, ok := users[username]
        if ok && user.Password == password {
            session, _ := store.Get(r, "session")
            session.Values["username"] = user.Username
            session.Values["role"] = user.Role
            session.Options = &sessions.Options{Path: "/", MaxAge: sessionDuration * 60, HttpOnly: true}
            session.Save(r, w)
            http.Redirect(w, r, "/", http.StatusFound)
            return
        }

        // Try DB users: treat username as email and set a normal session
        if u, exists := getUserByEmail(username); exists && u.Password != "" {
            if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)); err == nil {
                session, _ := store.Get(r, "session")
                // prefer displaying the DB user's name
                session.Values["username"] = u.Name
                session.Values["role"] = u.Role
                session.Options = &sessions.Options{Path: "/", MaxAge: sessionDuration * 60, HttpOnly: true}
                session.Save(r, w)
                http.Redirect(w, r, "/", http.StatusFound)
                return
            }
        }
        renderTemplate(w, r, "login", map[string]any{"Error": "Benutzername oder Passwort falsch."})
    }
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	// Löscht die Session und leitet zur Login-Seite weiter
	session, _ := store.Get(r, "session")
	session.Options.MaxAge = -1 // Löscht das Cookie
	session.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// adminOnly middleware: requires logged-in CSV user with role=admin
func adminOnly(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        session, _ := store.Get(r, "session")
        role, _ := session.Values["role"].(string)
        if role != "admin" && role != "Admin" && role != "ADMIN" {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
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
		renderTemplate(w, r, "clockInOutForm", data)
	}
}

// addUserHandler shows the add-user page
func addUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		depts := getDepartments()
		users := getUsers()
		renderTemplate(w, r, "addUser", struct {
			Departments []Department
			Users       []User
		}{depts, users})
	}
}

// editUserHandler shows or processes the edit-user page
func editUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.FormValue("id")
		u := getUser(id)
		depts := getDepartments()
		renderTemplate(w, r, "editUser", struct {
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
            r.FormValue("password"),
            r.FormValue("role"),
            r.FormValue("position"),
            r.FormValue("department_id"),
        )
        // update auto-checkout flag
        setUserAutoCheckout(id, r.FormValue("auto_checkout_midnight") == "on")
    }
    http.Redirect(w, r, "/addUser", http.StatusSeeOther)
}

// addActivityHandler shows the add-activity page
func addActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		activities := getActivities()
		renderTemplate(w, r, "addActivity", struct {
			Activities []Activity
		}{activities})
	}
}

// addDepartmentHandler shows the add-department page
func addDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		depts := getDepartments()
		renderTemplate(w, r, "addDepartment", struct {
			Departments []Department
		}{depts})
	}
}

// createUserHandler processes adding a new user
func createUserHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodPost {
        createUser(
            r.FormValue("name"),
            r.FormValue("stampkey"),
            r.FormValue("email"),
            r.FormValue("password"),
            r.FormValue("role"),
            r.FormValue("position"),
            r.FormValue("department_id"),
        )
        // Set auto-checkout flag if provided
        // Need the created user id; simplest: lookup by email+name (could be non-unique on name; email is unique)
        if email := r.FormValue("email"); email != "" {
            if u, ok := getUserByEmail(email); ok {
                setUserAutoCheckout(strconv.Itoa(u.ID), r.FormValue("auto_checkout_midnight") == "on")
            }
        }
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
	renderTemplate(w, r, "barcodes", data)
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

// passwordStampHandler allows stamping by email+password, then choosing activity buttons
func passwordStampHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// show login form
		renderTemplate(w, r, "passwordStamp", nil)
		return
	case http.MethodPost:
		email := r.FormValue("email")
		pwd := r.FormValue("pwd")
		activityID := r.FormValue("activity_id")
		u, ok := getUserByEmail(email)
		if !ok || u.Password == "" {
			renderTemplate(w, r, "passwordStamp", map[string]any{"Error": "Unbekannte E-Mail oder kein Passwort gesetzt."})
			return
		}
		// verify password
		if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(pwd)); err != nil {
			renderTemplate(w, r, "passwordStamp", map[string]any{"Error": "Falsches Passwort."})
			return
		}
		if activityID == "" {
			// show activities as buttons
			activities := getActivities()
			renderTemplate(w, r, "passwordStamp", map[string]any{
				"User":       u,
				"Activities": activities,
				"Pwd":        pwd, // keep for next post to avoid retyping
			})
			return
		}
		// stamp entry
		createEntry(strconv.Itoa(u.ID), activityID, time.Now())
		renderTemplate(w, r, "passwordStamp", map[string]any{"User": u, "Success": true})
		return
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
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

	pageData := struct {
		WorkHoursTable struct {
			Headers []string
			Rows    [][]interface{}
		}
		CurrentStatusTable struct {
			Headers []string
			Rows    [][]interface{}
		}
	}{
		WorkHoursTable: struct {
			Headers []string
			Rows    [][]interface{}
		}{
			Headers: []string{"User Name", "Work Date", "Work Hours"},
			Rows:    workRows,
		},
		CurrentStatusTable: struct {
			Headers []string
			Rows    [][]interface{}
		}{
			Headers: []string{"User Name", "Status", "Date"},
			Rows:    statusRows,
		},
	}

	renderTemplate(w, r, "layout", pageData)
}

// workHoursHandler shows the work hours table
func workHoursHandler(w http.ResponseWriter, r *http.Request) {
	data := getWorkHoursData()
	headers := []string{"User Name", "Work Date", "Work Hours"}
	rows := make([][]interface{}, len(data))
	for i, d := range data {
		rows[i] = []interface{}{d.UserName, d.WorkDate, d.WorkHours}
	}

	tableData := struct {
		Title   string
		Headers []string
		Rows    [][]interface{}
	}{
		Title:   "Work Hours Overview",
		Headers: headers,
		Rows:    rows,
	}

	renderTemplate(w, r, "workhours", tableData)
}

// currentStatusHandler shows who is currently clocked in/out
func currentStatusHandler(w http.ResponseWriter, r *http.Request) {
	data := getCurrentStatusData()
	headers := []string{"User Name", "Status", "Date"}
	rows := make([][]interface{}, len(data))
	for i, d := range data {
		rows[i] = []interface{}{d.UserName, d.Status, d.Date}
	}

	tableData := struct {
		Title   string
		Headers []string
		Rows    [][]interface{}
	}{
		Title:   "Current Status",
		Headers: headers,
		Rows:    rows,
	}

	renderTemplate(w, r, "currentstatus", tableData)
}

// scanHandler serves the barcode-scanning page
func scanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderTemplate(w, r, "scan", nil)
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
        // auto checkout at midnight if flagged and necessary
        ensureMidnightAutoCheckoutWithDB(db, userID, time.Now())
        stmt.Exec(now, activityID, userID)
    }
    tx.Commit()
    w.WriteHeader(http.StatusNoContent)
}

// Enhanced dashboard handler
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	deptSummary := getDepartmentSummary()
	timeTrends := getTimeTrackingTrends(30) // Last 30 days
	userActivity := getUserActivitySummary()

	// Calculate quick stats
	var totalUsers, totalHours, activeUsers int
	var avgHoursPerUser float64

	for _, dept := range deptSummary {
		totalUsers += dept.TotalUsers
		totalHours += int(dept.TotalHours)
	}

	if totalUsers > 0 {
		avgHoursPerUser = float64(totalHours) / float64(totalUsers)
	}

	// Count active users (users with activity in last 7 days)
	for _, user := range userActivity {
		if user.LastActivity != "" {
			activeUsers++
		}
	}

	data := struct {
		DepartmentSummary []DepartmentSummary
		TimeTrends        []TimeTrackingTrend
		UserActivity      []UserActivitySummary
		QuickStats        struct {
			TotalUsers      int
			TotalHours      int
			ActiveUsers     int
			AvgHoursPerUser float64
		}
	}{
		DepartmentSummary: deptSummary,
		TimeTrends:        timeTrends,
		UserActivity:      userActivity,
		QuickStats: struct {
			TotalUsers      int
			TotalHours      int
			ActiveUsers     int
			AvgHoursPerUser float64
		}{
			TotalUsers:      totalUsers,
			TotalHours:      totalHours,
			ActiveUsers:     activeUsers,
			AvgHoursPerUser: avgHoursPerUser,
		},
	}

	renderTemplate(w, r, "dashboard", data)
}

// Entries management handler
func entriesHandler(w http.ResponseWriter, r *http.Request) {
	entries := getEntriesWithDetails()
	users := getUsers()
	activities := getActivities()

	data := struct {
		Entries    []EntryDetail
		Users      []User
		Activities []Activity
	}{
		Entries:    entries,
		Users:      users,
		Activities: activities,
	}

	renderTemplate(w, r, "entries", data)
}

// Edit entry handler
func editEntryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.FormValue("id")
		entry := getEntry(id)
		users := getUsers()
		activities := getActivities()

		data := struct {
			Entry      EntryDetail
			Users      []User
			Activities []Activity
		}{
			Entry:      entry,
			Users:      users,
			Activities: activities,
		}

		renderTemplate(w, r, "editEntry", data)
		return
	}

	if r.Method == http.MethodPost {
		id := r.FormValue("id")
		userID := r.FormValue("user_id")
		activityID := r.FormValue("activity_id")
		date := r.FormValue("date")
		comment := r.FormValue("comment")

		updateEntry(id, userID, activityID, date, comment)
		http.Redirect(w, r, "/entries", http.StatusSeeOther)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// Edit activity handler
func editActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.FormValue("id")
		activity := getActivity(id)
		renderTemplate(w, r, "editActivity", activity)
		return
	}

	if r.Method == http.MethodPost {
		id := r.FormValue("id")
		updateActivity(id,
			r.FormValue("status"),
			r.FormValue("work"),
			r.FormValue("comment"),
		)
		http.Redirect(w, r, "/addActivity", http.StatusSeeOther)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// Edit department handler
func editDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		id := r.FormValue("id")
		dept := getDepartment(id)
		renderTemplate(w, r, "editDepartment", dept)
		return
	}

	if r.Method == http.MethodPost {
		id := r.FormValue("id")
		name := r.FormValue("name")
		updateDepartment(id, name)
		http.Redirect(w, r, "/addDepartment", http.StatusSeeOther)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// Delete handlers
func deleteEntryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.FormValue("id")
	deleteEntry(id)
	http.Redirect(w, r, "/entries", http.StatusSeeOther)
}

func deleteActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.FormValue("id")
	deleteActivity(id)
	http.Redirect(w, r, "/addActivity", http.StatusSeeOther)
}

func deleteDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.FormValue("id")
	deleteDepartment(id)
	http.Redirect(w, r, "/addDepartment", http.StatusSeeOther)
}

func deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.FormValue("id")
	deleteUser(id)
	http.Redirect(w, r, "/addUser", http.StatusSeeOther)
}

// downloadEntriesCSV streams recent entries with details as CSV (admin only)
func downloadEntriesCSV(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/csv")
    w.Header().Set("Content-Disposition", "attachment; filename=entries.csv")
    enc := csv.NewWriter(w)
    _ = enc.Write([]string{"ID", "User", "Department", "Activity", "Date", "Start", "End", "DurationHours", "Comment"})
    for _, e := range getEntriesWithDetails() {
        enc.Write([]string{strconv.Itoa(e.ID), e.UserName, e.Department, e.Activity, e.Date, e.Start, e.End, strconv.FormatFloat(e.Duration, 'f', 2, 64), e.Comment})
    }
    enc.Flush()
}

// downloadWorkHoursCSV streams aggregated work hours as CSV (admin only)
func downloadWorkHoursCSV(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/csv")
    w.Header().Set("Content-Disposition", "attachment; filename=work_hours.csv")
    enc := csv.NewWriter(w)
    _ = enc.Write([]string{"User", "Date", "WorkHours"})
    for _, wrow := range getWorkHoursData() {
        enc.Write([]string{wrow.UserName, wrow.WorkDate, strconv.FormatFloat(wrow.WorkHours, 'f', 2, 64)})
    }
    enc.Flush()
}

// myHistoryHandler lets a user view their own history by email+password with optional date range
func myHistoryHandler(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        renderTemplate(w, r, "myHistory", nil)
        return
    case http.MethodPost:
        email := r.FormValue("email")
        pwd := r.FormValue("pwd")
        from := r.FormValue("from")
        to := r.FormValue("to")
        u, ok := getUserByEmail(email)
        if !ok || u.Password == "" {
            renderTemplate(w, r, "myHistory", map[string]any{"Error": "Unknown email or no password set."})
            return
        }
        if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(pwd)); err != nil {
            renderTemplate(w, r, "myHistory", map[string]any{"Error": "Wrong password."})
            return
        }
        entries := getUserEntriesDetailed(u.ID, from, to)
        renderTemplate(w, r, "myHistory", map[string]any{
            "User":    u,
            "From":    from,
            "To":      to,
            "Entries": entries,
        })
        return
    default:
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
}
