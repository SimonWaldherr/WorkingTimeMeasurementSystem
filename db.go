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

type User struct {
	ID           int
	Name         string
	Email        string
	Position     string
	DepartmentID int
}

type Activity struct {
	ID      int
	Status  string
	Work    int
	Comment string
}

type Department struct {
	ID   int
	Name string
}

func getDB() *sql.DB {
	db, err := sql.Open("sqlite3", "time_tracking.db")
	if err != nil {
		log.Fatal(err)
	}
	return db
}

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

func createUser(name, email, position, departmentID string) {
	db := getDB()
	defer db.Close()

	stmt, err := db.Prepare("INSERT INTO users (name, email, position, 	department_id) VALUES (?, ?, ?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	deptID, err := strconv.Atoi(departmentID)
	if err != nil {
		log.Fatal(err)
	}

	_, err = stmt.Exec(name, email, position, deptID)
	if err != nil {
		log.Fatal(err)
	}
}

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
