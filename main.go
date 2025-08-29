package main

import (
	//"context"
	//"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"

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

// Calendar data structures for the calendar view
type CalendarDay struct {
	Day         int
	Date        string
	IsToday     bool
	IsOtherMonth bool
	Entries     []CalendarEntry
	TotalHours  float64
}

type CalendarEntry struct {
	Date       string
	UserName   string
	Activity   string
	Hours      float64
	IsWork     bool
}

type CalendarWeek struct {
	Days []CalendarDay
}

type CalendarMonth struct {
	Year      int
	Month     time.Month
	MonthName string
	Weeks     []CalendarWeek
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

// resolve DB user from session; falls back to matching by username
func currentDBUserFromSession(r *http.Request) (User, bool) {
    session, _ := store.Get(r, "session")
    if idVal, ok := session.Values["db_user_id"]; ok {
        switch v := idVal.(type) {
        case int:
            return getUser(strconv.Itoa(v)), true
        case int64:
            return getUser(strconv.Itoa(int(v))), true
        case string:
            return getUser(v), true
        }
    }
    if uname, ok := session.Values["username"].(string); ok && uname != "" {
        if u, ok2 := getUserByName(uname); ok2 {
            return u, true
        }
    }
    return User{}, false
}

func humanizeDuration(d time.Duration) string {
    if d < 0 { d = -d }
    hrs := int(d.Hours())
    mins := int(d.Minutes()) % 60
    if hrs > 0 {
        return strconv.Itoa(hrs) + "h " + strconv.Itoa(mins) + "m"
    }
    return strconv.Itoa(mins) + "m"
}

func basicAuthMiddleware(_ map[string]AuthUser, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        session, _ := store.Get(r, "session")
        username, ok := session.Values["username"].(string)
        if !ok || username == "" {
            http.Redirect(w, r, "/login", http.StatusFound)
            return
        }
        // Accept either CSV or DB-backed users; role is carried in session
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

    // calendar page
    mux.Handle("/calendar", basicAuthMiddleware(users, http.HandlerFunc(calendarHandler)))

    // Admin downloads page
    mux.Handle("/admin/downloads", adminOnly(http.HandlerFunc(adminDownloadsHandler)))
    
    // Enhanced download endpoints with filtering
    mux.Handle("/admin/download/entries", adminOnly(http.HandlerFunc(downloadEntriesEnhanced)))
    mux.Handle("/admin/download/workhours", adminOnly(http.HandlerFunc(downloadWorkHoursEnhanced)))
    mux.Handle("/admin/download/departments", adminOnly(http.HandlerFunc(downloadDepartmentSummary)))
    mux.Handle("/admin/download/useractivity", adminOnly(http.HandlerFunc(downloadUserActivity)))
    mux.Handle("/admin/download/trends", adminOnly(http.HandlerFunc(downloadTimeTrends)))
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
    // current user status (if we can resolve a DB user)
    type cur struct{ Status, Since string }
    var current *cur
    if u, ok := currentDBUserFromSession(r); ok {
        if st, at, ok2 := getCurrentStatusForUserID(u.ID); ok2 {
            current = &cur{Status: st, Since: humanizeDuration(time.Since(at))}
        }
    }
    data := struct {
        Users      []User
        Activities []Activity
        Current    *cur
    }{users, activities, current}
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
                session.Values["db_user_id"] = u.ID
                session.Values["db_user_email"] = u.Email
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
    // Clear session for both CSV and DB users and redirect to login
    session, _ := store.Get(r, "session")
    // reset values and set delete cookie explicitly
    session.Values = map[interface{}]interface{}{}
    session.Options = &sessions.Options{Path: "/", MaxAge: -1, HttpOnly: true}
    _ = session.Save(r, w)
    // additionally ensure cookie deletion
    http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", MaxAge: -1})
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
        type cur struct{ Status, Since string }
        var current *cur
        if u, ok := currentDBUserFromSession(r); ok {
            if st, at, ok2 := getCurrentStatusForUserID(u.ID); ok2 {
                current = &cur{Status: st, Since: humanizeDuration(time.Since(at))}
            }
        }
        data := struct {
            Users      []User
            Activities []Activity
            Current    *cur
        }{users, activities, current}
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

// calendarHandler shows the calendar view with working times
func calendarHandler(w http.ResponseWriter, r *http.Request) {
	// Get filter parameters
	selectedUserID := r.URL.Query().Get("user")
	selectedActivityID := r.URL.Query().Get("activity")
	monthParam := r.URL.Query().Get("month")
	
	// Parse month parameter or default to current month
	var targetDate time.Time
	if monthParam != "" {
		if parsed, err := time.Parse("2006-01", monthParam); err == nil {
			targetDate = parsed
		} else {
			targetDate = time.Now()
		}
	} else {
		targetDate = time.Now()
	}
	
	// Get calendar data
	calendarData := getCalendarData(targetDate, selectedUserID, selectedActivityID)
	
	data := struct {
		Users        []User
		Activities   []Activity
		CalendarData CalendarMonth
		SelectedUser string
		SelectedActivity string
		CurrentMonth string
		PrevMonth    string
		NextMonth    string
	}{
		Users:            getUsers(),
		Activities:       getActivities(),
		CalendarData:     calendarData,
		SelectedUser:     selectedUserID,
		SelectedActivity: selectedActivityID,
		CurrentMonth:     targetDate.Format("2006-01"),
		PrevMonth:        targetDate.AddDate(0, -1, 0).Format("2006-01"),
		NextMonth:        targetDate.AddDate(0, 1, 0).Format("2006-01"),
	}
	renderTemplate(w, r, "calendar", data)
}

// getCalendarData generates calendar data for a specific month with optional filters
func getCalendarData(targetDate time.Time, userFilter, activityFilter string) CalendarMonth {
	year := targetDate.Year()
	month := targetDate.Month()
	
	// Get first day of month
	firstDay := time.Date(year, month, 1, 0, 0, 0, 0, targetDate.Location())
	// Get last day of month
	lastDay := firstDay.AddDate(0, 1, -1)
	
	// Get first day of calendar (may be in previous month)
	// Start from Monday (1) to Sunday (0) 
	calendarStart := firstDay
	for calendarStart.Weekday() != time.Monday {
		calendarStart = calendarStart.AddDate(0, 0, -1)
	}
	
	// Get last day of calendar (may be in next month)
	calendarEnd := lastDay
	for calendarEnd.Weekday() != time.Sunday {
		calendarEnd = calendarEnd.AddDate(0, 0, 1)
	}
	
	// Get entries for the calendar period
	entries := getCalendarEntries(calendarStart, calendarEnd, userFilter, activityFilter)
	
	// Group entries by date
	entriesByDate := make(map[string][]CalendarEntry)
	for _, entry := range entries {
		dateKey := entry.Date[:10] // Extract YYYY-MM-DD part
		entriesByDate[dateKey] = append(entriesByDate[dateKey], entry)
	}
	
	// Build calendar structure
	var weeks []CalendarWeek
	current := calendarStart
	
	for current.Before(calendarEnd.AddDate(0, 0, 1)) {
		week := CalendarWeek{}
		
		// Build 7 days for this week
		for i := 0; i < 7; i++ {
			dateKey := current.Format("2006-01-02")
			dayEntries := entriesByDate[dateKey]
			
			totalHours := 0.0
			for _, entry := range dayEntries {
				totalHours += entry.Hours
			}
			
			day := CalendarDay{
				Day:          current.Day(),
				Date:         dateKey,
				IsToday:      current.Format("2006-01-02") == time.Now().Format("2006-01-02"),
				IsOtherMonth: current.Month() != month,
				Entries:      dayEntries,
				TotalHours:   totalHours,
			}
			
			week.Days = append(week.Days, day)
			current = current.AddDate(0, 0, 1)
		}
		
		weeks = append(weeks, week)
	}
	
	return CalendarMonth{
		Year:      year,
		Month:     month,
		MonthName: month.String(),
		Weeks:     weeks,
	}
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
    session, _ := store.Get(r, "session")
    if idVal, ok := session.Values["db_user_id"]; ok {
        // Logged-in DB user: no password needed
        uid := 0
        switch v := idVal.(type) {
        case int:
            uid = v
        case int64:
            uid = int(v)
        case string:
            uid, _ = strconv.Atoi(v)
        }
        u := getUser(strconv.Itoa(uid))
        switch r.Method {
        case http.MethodGet:
            activities := getActivities()
            var current any
            if st, at, ok2 := getCurrentStatusForUserID(u.ID); ok2 {
                current = map[string]string{"Status": st, "Since": humanizeDuration(time.Since(at))}
            }
            renderTemplate(w, r, "passwordStamp", map[string]any{
                "User":       u,
                "Activities": activities,
                "Current":    current,
            })
            return
        case http.MethodPost:
            activityID := r.FormValue("activity_id")
            if activityID == "" {
                activities := getActivities()
                var current any
                if st, at, ok2 := getCurrentStatusForUserID(u.ID); ok2 {
                    current = map[string]string{"Status": st, "Since": humanizeDuration(time.Since(at))}
                }
                renderTemplate(w, r, "passwordStamp", map[string]any{
                    "User":       u,
                    "Activities": activities,
                    "Current":    current,
                })
                return
            }
            createEntry(strconv.Itoa(u.ID), activityID, time.Now())
            var current any
            if st, at, ok2 := getCurrentStatusForUserID(u.ID); ok2 {
                current = map[string]string{"Status": st, "Since": humanizeDuration(time.Since(at))}
            }
            renderTemplate(w, r, "passwordStamp", map[string]any{"User": u, "Success": true, "Current": current})
            return
        default:
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }
    }
    // Fallback: email + password flow
    switch r.Method {
    case http.MethodGet:
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
        if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(pwd)); err != nil {
            renderTemplate(w, r, "passwordStamp", map[string]any{"Error": "Falsches Passwort."})
            return
        }
        if activityID == "" {
            activities := getActivities()
            var current any
            if st, at, ok2 := getCurrentStatusForUserID(u.ID); ok2 {
                current = map[string]string{"Status": st, "Since": humanizeDuration(time.Since(at))}
            }
            renderTemplate(w, r, "passwordStamp", map[string]any{
                "User":       u,
                "Activities": activities,
                "Pwd":        pwd,
                "Current":    current,
            })
            return
        }
        createEntry(strconv.Itoa(u.ID), activityID, time.Now())
        var current any
        if st, at, ok2 := getCurrentStatusForUserID(u.ID); ok2 {
            current = map[string]string{"Status": st, "Since": humanizeDuration(time.Since(at))}
        }
        renderTemplate(w, r, "passwordStamp", map[string]any{"User": u, "Success": true, "Current": current})
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
    // Admin sees all; regular users see their own
    session, _ := store.Get(r, "session")
    role, _ := session.Values["role"].(string)
    var data []WorkHoursData
    if role == "admin" || role == "Admin" || role == "ADMIN" {
        data = getWorkHoursData()
    } else if u, ok := currentDBUserFromSession(r); ok {
        data = getWorkHoursDataForUser(u.Name)
    } else {
        data = nil
    }
    headers := []string{"User Name", "Work Date", "Work Hours"}
    rows := make([][]interface{}, len(data))
    for i, d := range data {
        rows[i] = []interface{}{d.UserName, d.WorkDate, d.WorkHours}
    }
    tableData := struct {
        Title   string
        Headers []string
        Rows    [][]interface{}
    }{"Work Hours Overview", headers, rows}
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

// adminDownloadsHandler displays the enhanced downloads page for admins
func adminDownloadsHandler(w http.ResponseWriter, r *http.Request) {
    users := getUsers()
    activities := getActivities()
    departments := getDepartments()

    data := struct {
        Users       []User
        Activities  []Activity
        Departments []Department
    }{
        Users:       users,
        Activities:  activities,
        Departments: departments,
    }

    renderTemplate(w, r, "downloads", data)
}

// downloadEntriesEnhanced provides enhanced time entries download with filtering
func downloadEntriesEnhanced(w http.ResponseWriter, r *http.Request) {
    // Parse query parameters
    fromDate := r.URL.Query().Get("fromDate")
    toDate := r.URL.Query().Get("toDate")
    department := r.URL.Query().Get("department")
    user := r.URL.Query().Get("user")
    activity := r.URL.Query().Get("activity")
    format := r.URL.Query().Get("format")
    limit := r.URL.Query().Get("limit")
    
    if format == "" {
        format = "csv"
    }

    // Get filtered entries
    entries := getEntriesWithDetailsFiltered(fromDate, toDate, department, user, activity, limit)

    // Handle preview format
    if format == "preview" {
        renderPreviewTable(w, entries, "entries")
        return
    }

    // Generate filename with timestamp
    timestamp := time.Now().Format("2006-01-02_15-04-05")
    var filename string
    var contentType string

    switch format {
    case "json":
        filename = fmt.Sprintf("time_entries_%s.json", timestamp)
        contentType = "application/json"
        w.Header().Set("Content-Type", contentType)
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        json.NewEncoder(w).Encode(entries)
        
    case "excel":
        filename = fmt.Sprintf("time_entries_%s.csv", timestamp)
        contentType = "text/csv"
        w.Header().Set("Content-Type", contentType)
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        enc := csv.NewWriter(w)
        // Excel-friendly CSV with BOM for UTF-8
        w.Write([]byte{0xEF, 0xBB, 0xBF})
        _ = enc.Write([]string{"ID", "User", "Department", "Activity", "Date", "Start", "End", "Duration Hours", "Comment"})
        for _, e := range entries {
            enc.Write([]string{strconv.Itoa(e.ID), e.UserName, e.Department, e.Activity, e.Date, e.Start, e.End, strconv.FormatFloat(e.Duration, 'f', 2, 64), e.Comment})
        }
        enc.Flush()
        
    default: // csv
        filename = fmt.Sprintf("time_entries_%s.csv", timestamp)
        contentType = "text/csv"
        w.Header().Set("Content-Type", contentType)
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        enc := csv.NewWriter(w)
        _ = enc.Write([]string{"ID", "User", "Department", "Activity", "Date", "Start", "End", "Duration Hours", "Comment"})
        for _, e := range entries {
            enc.Write([]string{strconv.Itoa(e.ID), e.UserName, e.Department, e.Activity, e.Date, e.Start, e.End, strconv.FormatFloat(e.Duration, 'f', 2, 64), e.Comment})
        }
        enc.Flush()
    }
}

// downloadWorkHoursEnhanced provides enhanced work hours download with filtering
func downloadWorkHoursEnhanced(w http.ResponseWriter, r *http.Request) {
    // Parse query parameters
    fromDate := r.URL.Query().Get("fromDate")
    toDate := r.URL.Query().Get("toDate")
    user := r.URL.Query().Get("user")
    format := r.URL.Query().Get("format")
    limit := r.URL.Query().Get("limit")
    
    if format == "" {
        format = "csv"
    }

    // Get filtered work hours data
    workHours := getWorkHoursDataFiltered(fromDate, toDate, user, limit)

    // Handle preview format
    if format == "preview" {
        renderPreviewTableWorkHours(w, workHours)
        return
    }

    // Generate filename with timestamp
    timestamp := time.Now().Format("2006-01-02_15-04-05")
    var filename string
    var contentType string

    switch format {
    case "json":
        filename = fmt.Sprintf("work_hours_%s.json", timestamp)
        contentType = "application/json"
        w.Header().Set("Content-Type", contentType)
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        json.NewEncoder(w).Encode(workHours)
        
    case "excel":
        filename = fmt.Sprintf("work_hours_%s.csv", timestamp)
        contentType = "text/csv"
        w.Header().Set("Content-Type", contentType)
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        enc := csv.NewWriter(w)
        // Excel-friendly CSV with BOM for UTF-8
        w.Write([]byte{0xEF, 0xBB, 0xBF})
        _ = enc.Write([]string{"User", "Date", "Work Hours"})
        for _, wh := range workHours {
            enc.Write([]string{wh.UserName, wh.WorkDate, strconv.FormatFloat(wh.WorkHours, 'f', 2, 64)})
        }
        enc.Flush()
        
    default: // csv
        filename = fmt.Sprintf("work_hours_%s.csv", timestamp)
        contentType = "text/csv"
        w.Header().Set("Content-Type", contentType)
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        enc := csv.NewWriter(w)
        _ = enc.Write([]string{"User", "Date", "Work Hours"})
        for _, wh := range workHours {
            enc.Write([]string{wh.UserName, wh.WorkDate, strconv.FormatFloat(wh.WorkHours, 'f', 2, 64)})
        }
        enc.Flush()
    }
}

// downloadDepartmentSummary provides department summary download
func downloadDepartmentSummary(w http.ResponseWriter, r *http.Request) {
    format := r.URL.Query().Get("format")
    if format == "" {
        format = "csv"
    }

    departments := getDepartmentSummary()
    timestamp := time.Now().Format("2006-01-02_15-04-05")

    switch format {
    case "json":
        filename := fmt.Sprintf("department_summary_%s.json", timestamp)
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        json.NewEncoder(w).Encode(departments)
        
    default: // csv
        filename := fmt.Sprintf("department_summary_%s.csv", timestamp)
        w.Header().Set("Content-Type", "text/csv")
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        enc := csv.NewWriter(w)
        _ = enc.Write([]string{"Department", "Total Users", "Total Hours", "Avg Hours Per User"})
        for _, d := range departments {
            enc.Write([]string{d.DepartmentName, strconv.Itoa(d.TotalUsers), strconv.FormatFloat(d.TotalHours, 'f', 2, 64), strconv.FormatFloat(d.AvgHoursPerUser, 'f', 2, 64)})
        }
        enc.Flush()
    }
}

// downloadUserActivity provides user activity report download
func downloadUserActivity(w http.ResponseWriter, r *http.Request) {
    format := r.URL.Query().Get("format")
    if format == "" {
        format = "csv"
    }

    userActivity := getUserActivitySummary()
    timestamp := time.Now().Format("2006-01-02_15-04-05")

    switch format {
    case "json":
        filename := fmt.Sprintf("user_activity_%s.json", timestamp)
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        json.NewEncoder(w).Encode(userActivity)
        
    default: // csv
        filename := fmt.Sprintf("user_activity_%s.csv", timestamp)
        w.Header().Set("Content-Type", "text/csv")
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        enc := csv.NewWriter(w)
        _ = enc.Write([]string{"User", "Department", "Total Work Hours", "Total Break Hours", "Last Activity", "Status"})
        for _, u := range userActivity {
            enc.Write([]string{u.UserName, u.Department, strconv.FormatFloat(u.TotalWorkHours, 'f', 2, 64), strconv.FormatFloat(u.TotalBreakHours, 'f', 2, 64), u.LastActivity, u.Status})
        }
        enc.Flush()
    }
}

// downloadTimeTrends provides time trends report download
func downloadTimeTrends(w http.ResponseWriter, r *http.Request) {
    format := r.URL.Query().Get("format")
    if format == "" {
        format = "csv"
    }

    trends := getTimeTrackingTrends(30) // Last 30 days
    timestamp := time.Now().Format("2006-01-02_15-04-05")

    switch format {
    case "json":
        filename := fmt.Sprintf("time_trends_%s.json", timestamp)
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        json.NewEncoder(w).Encode(trends)
        
    default: // csv
        filename := fmt.Sprintf("time_trends_%s.csv", timestamp)
        w.Header().Set("Content-Type", "text/csv")
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
        
        enc := csv.NewWriter(w)
        _ = enc.Write([]string{"Date", "Total Hours", "Active Users", "Work Entries", "Break Entries"})
        for _, t := range trends {
            enc.Write([]string{t.Date, strconv.FormatFloat(t.TotalHours, 'f', 2, 64), strconv.Itoa(t.ActiveUsers), strconv.Itoa(t.WorkEntries), strconv.Itoa(t.BreakEntries)})
        }
        enc.Flush()
    }
}

// renderPreviewTable renders a preview table for entries
func renderPreviewTable(w http.ResponseWriter, entries []EntryDetail, tableType string) {
    w.Header().Set("Content-Type", "text/html")
    
    html := `<table class="table table-striped table-hover">
        <thead class="table-dark">
            <tr>
                <th>ID</th>
                <th>User</th>
                <th>Department</th>
                <th>Activity</th>
                <th>Date</th>
                <th>Start</th>
                <th>End</th>
                <th>Duration</th>
                <th>Comment</th>
            </tr>
        </thead>
        <tbody>`
    
    for _, e := range entries {
        html += fmt.Sprintf(`
            <tr>
                <td>%d</td>
                <td>%s</td>
                <td>%s</td>
                <td>%s</td>
                <td>%s</td>
                <td>%s</td>
                <td>%s</td>
                <td>%.2f h</td>
                <td>%s</td>
            </tr>`, e.ID, e.UserName, e.Department, e.Activity, e.Date, e.Start, e.End, e.Duration, e.Comment)
    }
    
    html += `</tbody></table>`
    
    if len(entries) == 0 {
        html = `<div class="alert alert-info">No entries found for the selected criteria.</div>`
    }
    
    w.Write([]byte(html))
}

// renderPreviewTableWorkHours renders a preview table for work hours
func renderPreviewTableWorkHours(w http.ResponseWriter, workHours []WorkHoursData) {
    w.Header().Set("Content-Type", "text/html")
    
    html := `<table class="table table-striped table-hover">
        <thead class="table-dark">
            <tr>
                <th>User</th>
                <th>Date</th>
                <th>Work Hours</th>
            </tr>
        </thead>
        <tbody>`
    
    for _, wh := range workHours {
        html += fmt.Sprintf(`
            <tr>
                <td>%s</td>
                <td>%s</td>
                <td>%.2f h</td>
            </tr>`, wh.UserName, wh.WorkDate, wh.WorkHours)
    }
    
    html += `</tbody></table>`
    
    if len(workHours) == 0 {
        html = `<div class="alert alert-info">No work hours data found for the selected criteria.</div>`
    }
    
    w.Write([]byte(html))
}

// myHistoryHandler lets a user view their own history by email+password with optional date range
func myHistoryHandler(w http.ResponseWriter, r *http.Request) {
    session, _ := store.Get(r, "session")
    if idVal, ok := session.Values["db_user_id"]; ok {
        // Logged-in DB user path: no password required
        uid := 0
        switch v := idVal.(type) {
        case int:
            uid = v
        case int64:
            uid = int(v)
        case string:
            uid, _ = strconv.Atoi(v)
        }
        u := getUser(strconv.Itoa(uid))
        if r.Method == http.MethodGet {
            renderTemplate(w, r, "myHistory", map[string]any{"User": u})
            return
        }
        if r.Method == http.MethodPost {
            from := r.FormValue("from")
            to := r.FormValue("to")
            entries := getUserEntriesDetailed(u.ID, from, to)
            renderTemplate(w, r, "myHistory", map[string]any{
                "User":    u,
                "From":    from,
                "To":      to,
                "Entries": entries,
            })
            return
        }
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    // Fallback: email + password flow
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
