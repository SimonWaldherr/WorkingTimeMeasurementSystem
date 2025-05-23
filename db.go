package main

import (
	"database/sql"
	_ "embed"
	"log"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed timetrack_init.sql
var embeddedSQL string

// User is a struct that represents a user in the database
type User struct {
	ID           int
	Stampkey     string
	Name         string
	Email        string
	Position     string
	DepartmentID int
}

// Activity is a struct that represents an activity in the database
type Activity struct {
	ID      int
	Status  string
	Work    int
	Comment string
}

// Department is a struct that represents a department in the database
type Department struct {
	ID   int
	Name string
}

// getDB returns a database connection
func getDB() *sql.DB {
	db, err := sql.Open("sqlite3", "time_tracking.db")
	if err != nil {
		log.Fatal(err)
	}
	return db
}

// createDatabaseAndTables creates the database and tables
func createDatabaseAndTables() {
	db := getDB()
	defer db.Close()

	createTables := strings.Split(embeddedSQL, ";\n\n")

	for _, query := range createTables {
		_, err := db.Exec(query)
		if err != nil {
			log.Fatalf("Failed to create table: %s\n%s", query, err)
		}
	}
}

// getUsers returns a slice of all users in the database
func getUsers() []User {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(`SELECT id, name, email, position, department_id FROM users`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.Position, &user.DepartmentID)
		if err != nil {
			log.Fatal(err)
		}
		users = append(users, user)
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	return users
}

// getActivities returns a slice of all activities in the database
func getActivities() []Activity {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(`SELECT id, status, work, comment FROM type`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var activities []Activity
	for rows.Next() {
		var activity Activity
		err := rows.Scan(&activity.ID, &activity.Status, &activity.Work, &activity.Comment)
		if err != nil {
			log.Fatal(err)
		}
		activities = append(activities, activity)
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	return activities
}

func getEntries() []Entry {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(`SELECT id, user_id, type_id, date FROM entries`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var entry Entry
		err := rows.Scan(&entry.ID, &entry.UserID, &entry.ActivityID, &entry.Date)
		if err != nil {
			log.Fatal(err)
		}
		entries = append(entries, entry)
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	return entries
}

// getDepartments returns a slice of all departments in the database
func getDepartments() []Department {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(`SELECT id, name FROM departments`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var departments []Department
	for rows.Next() {
		var department Department
		err := rows.Scan(&department.ID, &department.Name)
		if err != nil {
			log.Fatal(err)
		}
		departments = append(departments, department)
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

	return departments
}

// getUser returns a single user from the database
func getUser(id string) User {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare(`SELECT id, name, stampkey, email, position, department_id FROM users WHERE id = ?`)
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	var user User
	err = stmt.QueryRow(id).Scan(&user.ID, &user.Name, &user.Stampkey, &user.Email, &user.Position, &user.DepartmentID)
	if err != nil {
		log.Fatal(err)
	}

	return user
}

// getActivity returns a single activity from the database
func getActivity(id string) Activity {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare(`SELECT id, status, work, comment FROM type WHERE id = ?`)
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	var activity Activity
	err = stmt.QueryRow(id).Scan(&activity.ID, &activity.Status, &activity.Work, &activity.Comment)
	if err != nil {
		log.Fatal(err)
	}

	return activity
}

// getDepartment returns a single department from the database
func getDepartment(id string) Department {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare(`SELECT id, name FROM departments WHERE id = ?`)
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	var department Department
	err = stmt.QueryRow(id).Scan(&department.ID, &department.Name)
	if err != nil {
		log.Fatal(err)
	}

	return department
}

// getUserIDFromStampKey returns the user ID from the stamp key
func getUserIDFromStampKey(stampKey string) string {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare(`SELECT id FROM users WHERE stampkey = ?`)
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	var userID string
	err = stmt.QueryRow(stampKey).Scan(&userID)
	if err != nil {
		log.Println(err)
	}

	return userID
}

// createUser creates a new user in the database
func createUser(name, stampkey, email, position, departmentID string) {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare("INSERT INTO users (name, stampkey, email, position, department_id) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	deptID, err := strconv.Atoi(departmentID)
	if err != nil {
		log.Fatal(err)
	}

	_, err = stmt.Exec(name, stampkey, email, position, deptID)
	if err != nil {
		log.Fatal(err)
	}
}

// createActivity creates a new activity in the database
func createActivity(status string, work string, comment string) {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare("INSERT INTO type (status, work, comment) VALUES (?, ?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	workInt, err := strconv.Atoi(work)
	if err != nil {
		log.Fatal(err)
	}

	_, err = stmt.Exec(status, workInt, comment)
	if err != nil {
		log.Fatal(err)
	}
}

// createDepartment creates a new department in the database
func createDepartment(name string) {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare("INSERT INTO departments (name) VALUES (?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(name)
	if err != nil {
		log.Fatal(err)
	}
}

// updateUser updates a user in the database
func updateUser(id, name, stampkey, email, position, departmentID string) {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare("UPDATE users SET name = ?, stampkey = ?, email = ?, position = ?, department_id = ? WHERE id = ?")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	deptID, err := strconv.Atoi(departmentID)
	if err != nil {
		log.Fatal(err)
	}

	_, err = stmt.Exec(name, email, position, deptID, id)
	if err != nil {
		log.Fatal(err)
	}
}

// updateActivity updates an activity in the database
func updateActivity(id, status, work, comment string) {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare("UPDATE type SET status = ?, work = ?, comment = ? WHERE id = ?")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	workInt, err := strconv.Atoi(work)
	if err != nil {
		log.Fatal(err)
	}

	_, err = stmt.Exec(status, workInt, comment, id)
	if err != nil {
		log.Fatal(err)
	}
}

// getWorkHoursData returns the data for the work hours view
func getWorkHoursData() []WorkHoursData {
	db := getDB()
	defer db.Close()

	rows, err := db.Query("SELECT user_name, work_date, work_hours FROM work_hours;")
	if err != nil {
		log.Fatal("Error querying work_hours_view")
		return nil
	}
	defer rows.Close()

	var workHoursData []WorkHoursData
	for rows.Next() {
		var data WorkHoursData
		err = rows.Scan(&data.UserName, &data.WorkDate, &data.WorkHours)
		if err != nil {
			log.Fatal("Error scanning work_hours_view data")
			return nil
		}
		workHoursData = append(workHoursData, data)
	}
	return workHoursData
}

// getCurrentStatusData returns the data for the current status view
func getCurrentStatusData() []CurrentStatusData {
	db := getDB()
	defer db.Close()

	rows, err := db.Query("SELECT user_name, status, date FROM current_status;")
	if err != nil {
		log.Fatal("Error querying current_status")
		return nil
	}
	defer rows.Close()

	var currentStatusData []CurrentStatusData
	for rows.Next() {
		var data CurrentStatusData
		err = rows.Scan(&data.UserName, &data.Status, &data.Date)
		if err != nil {
			log.Fatal("Error scanning current_status_view data")
			return nil
		}
		currentStatusData = append(currentStatusData, data)
	}
	return currentStatusData
}
