package main

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"fmt"
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

// AuthUser is a struct that represents a user in the database
type AuthUser struct {
	Username string
	Password string
	Role     string
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

		authHeaderParts := strings.Split(authHeader, " ")
		if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "basic" {
			unauthorized(w)
			return
		}

		payload, err := base64.StdEncoding.DecodeString(authHeaderParts[1])
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

		// Add role to the request context if needed
		ctx := r.Context()
		ctx = context.WithValue(ctx, "role", user.Role)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// unauthorized writes the unauthorized response
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte("Unauthorized\n"))
}

// init initializes the database
func init() {
	createDatabaseAndTables()
}

func main() {
	users, err := loadCredentials("credentials.csv")
	if err != nil {
		fmt.Println("Error loading credentials:", err)
		return
	}

	mux := http.NewServeMux()

	mux.Handle("/", http.HandlerFunc(indexHandler))
	mux.Handle("/addUser", http.HandlerFunc(addUserHandler))
	mux.Handle("/editUser", http.HandlerFunc(editUserHandler))
	mux.Handle("/addActivity", http.HandlerFunc(addActivityHandler))
	mux.Handle("/addDepartment", http.HandlerFunc(addDepartmentHandler))
	mux.Handle("/clockInOutForm", http.HandlerFunc(clockInOutForm))
	mux.Handle("/createUser", basicAuthMiddleware(users, http.HandlerFunc(createUserHandler)))
	mux.Handle("/createActivity", basicAuthMiddleware(users, http.HandlerFunc(createActivityHandler)))
	mux.Handle("/createDepartment", basicAuthMiddleware(users, http.HandlerFunc(createDepartmentHandler)))
	mux.Handle("/clockInOut", http.HandlerFunc(clockInOut))
	mux.Handle("/work_hours", basicAuthMiddleware(users, http.HandlerFunc(workHoursHandler)))
	mux.Handle("/current_status", basicAuthMiddleware(users, http.HandlerFunc(currentStatusHandler)))

	log.Fatal(http.ListenAndServe(":8080", mux))
}

// indexHandler handles the index page
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

// clockInOutForm handles the clock in/out form page
func clockInOutForm(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		users := getUsers()
		data := struct {
			Users []User
		}{
			Users: users,
		}
		renderTemplate(w, "clockInOutForm", data)
	}
}

// addUserHandler handles the add user page
func addUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		departments := getDepartments()
		data := struct {
			Departments []Department
		}{
			Departments: departments,
		}
		renderTemplate(w, "addUser", data)
	}
}

// addActivityHandler handles the add activity page
func addActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderTemplate(w, "addActivity", nil)
	}
}

// addDepartmentHandler handles the add department page
func addDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderTemplate(w, "addDepartment", nil)
	}
}

// createUserHandler handles the create user page
func createUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		name := r.FormValue("name")
		stampkey := r.FormValue("stampkey")
		email := r.FormValue("email")
		position := r.FormValue("position")
		departmentID := r.FormValue("department_id")

		createUser(name, stampkey, email, position, departmentID)
	}

	http.Redirect(w, r, "/addUser", http.StatusSeeOther)
}

// editUserHandler handles the edit user page
func editUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		id := r.FormValue("id")

		user := getUser(id)
		departments := getDepartments()
		data := struct {
			User        User
			Departments []Department
		}{
			User:        user,
			Departments: departments,
		}
		renderTemplate(w, "editUser", data)
	}

	if r.Method == "POST" {
		id := r.FormValue("id")
		name := r.FormValue("name")
		stampkey := r.FormValue("stampkey")
		email := r.FormValue("email")
		position := r.FormValue("position")
		departmentID := r.FormValue("department_id")

		updateUser(id, name, stampkey, email, position, departmentID)
	}

	http.Redirect(w, r, "/addUser", http.StatusSeeOther)
}

// createActivityHandler handles the create activity page
func createActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		status := r.FormValue("status")
		work := r.FormValue("work")
		comment := r.FormValue("comment")

		createActivity(status, work, comment)
	}

	http.Redirect(w, r, "/addActivity", http.StatusSeeOther)
}

// createDepartmentHandler handles the create department page
func createDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		name := r.FormValue("name")

		createDepartment(name)
	}

	http.Redirect(w, r, "/addDepartment", http.StatusSeeOther)
}

// clockInOut handles the clock in/out page
func clockInOut(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()

		userID := r.FormValue("user_id")
		stampKey := r.FormValue("stampkey")
		activityID := r.FormValue("activity_id")

		if (userID != "" || stampKey != "") && activityID != "" {
			db := getDB()
			defer db.Close()

			if userID == "" && stampKey != "" {
				userID = getUserIDFromStampKey(stampKey)
			}

			stmt, err := db.Prepare("INSERT INTO entries (date, type_id, user_id) VALUES (?, ?, ?)")
			if err != nil {
				log.Fatal(err)
			}
			defer stmt.Close()

			userIDInt, err := strconv.Atoi(userID)
			if err != nil {
				log.Fatal(err)
			}

			activityIDInt, err := strconv.Atoi(activityID)
			if err != nil {
				log.Fatal(err)
			}

			now := time.Now().Format(time.RFC3339)
			_, err = stmt.Exec(now, activityIDInt, userIDInt)
			if err != nil {
				log.Fatal(err)
			}

			referer := r.Header.Get("Referer")
			if referer == "" {
				referer = "/"
			}
			http.Redirect(w, r, referer, http.StatusSeeOther)
		} else {
			http.Error(w, "Invalid input", http.StatusBadRequest)
		}
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// workHoursHandler handles the work hours page
func workHoursHandler(w http.ResponseWriter, r *http.Request) {
	workHoursData := getWorkHoursData()

	headers := []string{"User Name", "Work Date", "Work Hours"}
	rows := make([][]interface{}, len(workHoursData))

	for i, data := range workHoursData {
		rows[i] = []interface{}{data.UserName, data.WorkDate, data.WorkHours}
	}

	tableData := TableData{Headers: headers, Rows: rows}
	err := renderHTMLTable(w, tableData)
	if err != nil {
		http.Error(w, "Error rendering work hours table", http.StatusInternalServerError)
		return
	}

}

// currentStatusHandler handles the current status page
func currentStatusHandler(w http.ResponseWriter, r *http.Request) {
	currentStatusData := getCurrentStatusData()

	headers := []string{"User Name", "Status", "Date"}
	rows := make([][]interface{}, len(currentStatusData))

	for i, data := range currentStatusData {
		rows[i] = []interface{}{data.UserName, data.Status, data.Date}
	}

	tableData := TableData{Headers: headers, Rows: rows}
	err := renderHTMLTable(w, tableData)
	if err != nil {
		http.Error(w, "Error rendering current status table", http.StatusInternalServerError)
		return
	}

}

type TableData struct {
	Headers []string
	Rows    [][]interface{}
}

func renderHTMLTable(w io.Writer, tableData TableData) error {
	tmpl := `
		<table>
			<thead>
				<tr>
					{{ range .Headers }}
						<th>{{ . }}</th>
					{{ end }}
				</tr>
			</thead>
			<tbody>
				{{ range .Rows }}
					<tr>
						{{ range . }}
							<td>{{ . }}</td>
						{{ end }}
					</tr>
				{{ end }}
			</tbody>
		</table>
	`
	t, err := template.New("table").Parse(tmpl)
	if err != nil {
		return err
	}

	err = t.Execute(w, tableData)
	return err
}
