package main

import (
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type WorkHoursData struct {
	UserName  string
	WorkDate  string
	WorkHours float64
}

type CurrentStatusData struct {
	UserName string
	Status   string
	Date     string
}

func init() {
	createDatabaseAndTables()
}

func main() {
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/addUser", addUserHandler)
	http.HandleFunc("/addActivity", addActivityHandler)
	http.HandleFunc("/addDepartment", addDepartmentHandler)
	http.HandleFunc("/createUser", createUserHandler)
	http.HandleFunc("/createActivity", createActivityHandler)
	http.HandleFunc("/createDepartment", createDepartmentHandler)
	http.HandleFunc("/clockInOut", clockInOut)
	http.HandleFunc("/work_hours", workHoursHandler)
	http.HandleFunc("/current_status", currentStatusHandler)

	log.Fatal(http.ListenAndServe(":8080", nil))
}

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

func addActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderTemplate(w, "addActivity", nil)
	}
}

func addDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderTemplate(w, "addDepartment", nil)
	}
}

func createUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		name := r.FormValue("name")
		email := r.FormValue("email")
		position := r.FormValue("position")
		departmentID := r.FormValue("department_id")

		createUser(name, email, position, departmentID)
	}

	http.Redirect(w, r, "/addUser", http.StatusSeeOther)
}

func createActivityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		status := r.FormValue("status")
		work := r.FormValue("work")
		comment := r.FormValue("comment")

		createActivity(status, work, comment)
	}

	http.Redirect(w, r, "/addActivity", http.StatusSeeOther)
}

func createDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		name := r.FormValue("name")

		createDepartment(name)
	}

	http.Redirect(w, r, "/addDepartment", http.StatusSeeOther)
}

func clockInOut(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()

		userID := r.FormValue("user_id")
		activityID := r.FormValue("activity_id")

		if userID != "" && activityID != "" {
			db := getDB()
			defer db.Close()

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

			http.Redirect(w, r, "/", http.StatusSeeOther)
		} else {
			http.Error(w, "Invalid input", http.StatusBadRequest)
		}
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

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
