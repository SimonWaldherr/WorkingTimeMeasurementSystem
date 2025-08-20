package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"regexp"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	_ "github.com/mattn/go-sqlite3"
	_ "github.com/go-sql-driver/mysql"
)

//---------------------------------------------------------------------
// eingebettete Schema-Dateien
//---------------------------------------------------------------------

//go:embed timetrack_init.sql
var embeddedSQLiteSchema string

//go:embed timetrack_init.mssql.sql
var embeddedMSSQLSchema string

//go:embed timetrack_init.mariadb.sql
var embeddedMariaDBSchema string

//go:embed timetrack_tenant.sql
var embeddedSQLiteTenantSchema string

//go:embed timetrack_tenant_mssql.sql
var embeddedMSSQLTenantSchema string

//go:embed timetrack_tenant_mariadb.sql
var embeddedMariaDBTenantSchema string

//---------------------------------------------------------------------
// globale Konfiguration
//---------------------------------------------------------------------

var (
	dbBackend            string // "sqlite" | "mssql"
	sqlitePath           string
	mssqlServer, mssqlDB string
	mssqlUser, mssqlPass string
	mssqlPort            int
	mariadbHost, mariadbDB string
	mariadbUser, mariadbPass string
	mariadbPort            int
)

// Hilfsfunktionen
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func atoiDefault(s string, def int) int {
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	return def
}

//---------------------------------------------------------------------
// init: Konfiguration einlesen + ggf. Schema anlegen
//---------------------------------------------------------------------

func init() {
	// 1. Backend bestimmen
	dbBackend = strings.ToLower(getenv("DB_BACKEND", "mssql"))

	// 2. Backend-spezifische Defaults
	switch dbBackend {
	case "mssql":
		mssqlServer = getenv("MSSQL_SERVER", "sql-cluster-05")
		mssqlDB = getenv("MSSQL_DATABASE", "wtm")
		mssqlUser = getenv("MSSQL_USER", `johndoe`)
		mssqlPass = getenv("MSSQL_PASSWORD", "secret")
		mssqlPort = atoiDefault(getenv("MSSQL_PORT", "1433"), 1433)
	case "mariadb", "mysql":
		mariadbHost = getenv("MARIADB_HOST", "127.0.0.1")
		mariadbDB = getenv("MARIADB_DATABASE", "wtm")
		mariadbUser = getenv("MARIADB_USER", "wtm")
		mariadbPass = getenv("MARIADB_PASSWORD", "secret")
		mariadbPort = atoiDefault(getenv("MARIADB_PORT", "3306"), 3306)
	default: // sqlite
		sqlitePath = getenv("SQLITE_PATH", "time_tracking.db")
	}

	// 3. Tabellen / Views anlegen (nur wenn sinnvoll)
	createDatabaseAndTables()
}

//---------------------------------------------------------------------
// DB-Verbindung
//---------------------------------------------------------------------

func getDB() *sql.DB {
	var (
		driver string
		dsn    string
	)

	switch dbBackend {
	case "mssql":
		driver = "sqlserver"
		dsn = fmt.Sprintf(
			"server=%s;database=%s;user id=%s;password=%s;port=%d;encrypt=disable",
			mssqlServer, mssqlDB, mssqlUser, mssqlPass, mssqlPort,
		)
	case "mariadb", "mysql":
		driver = "mysql"
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&loc=Local",
			mariadbUser, mariadbPass, mariadbHost, mariadbPort, mariadbDB,
		)
	default: // sqlite
		driver = "sqlite3"
		dsn = sqlitePath
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		log.Fatalf("DB-Open-Fehler: %v", err)
	}
	return db
}

//---------------------------------------------------------------------
// Hilfskürzel: richtiger Tabellen/Vie­w-Name abhängig vom Backend
//---------------------------------------------------------------------

func tbl(name string) string {
	if dbBackend == "mssql" {
		return "wtm.wtm." + name // alle Tabellen liegen dort
	}
	return name
}

// tblWithTenant returns table name with tenant context for multi-tenant queries
func tblWithTenant(name string, tenantID int) string {
	return tbl(name) // For now, we'll use tenant_id column filtering
}

//---------------------------------------------------------------------
// Schema anlegen
//---------------------------------------------------------------------

func createDatabaseAndTables() {
	switch dbBackend {
	case "sqlite":
		execBatches(embeddedSQLiteSchema, ";\n")
		if getConfig().Features.MultiTenant && getConfig().Database.AutoMigrate {
			execBatches(embeddedSQLiteTenantSchema, ";\n")
		}
	case "mssql":
		if os.Getenv("DB_AUTO_MIGRATE") == "1" {
			//execBatches(embeddedMSSQLSchema, "\nGO")
		}
		if getConfig().Features.MultiTenant && getConfig().Database.AutoMigrate {
			execBatches(embeddedMSSQLTenantSchema, "\nGO")
		}
	case "mariadb", "mysql":
		execBatches(embeddedMariaDBSchema, ";\n")
		if getConfig().Features.MultiTenant && getConfig().Database.AutoMigrate {
			execBatches(embeddedMariaDBTenantSchema, ";\n")
		}
	}
}

func execBatches(script, sep string) {
	db := getDB()
	defer db.Close()

	for _, stmt := range strings.Split(script, sep) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			log.Printf("Schema-Batch-Fehler: %v\n%s\n", err, stmt)
		}
	}
}

// adaptQuery replaces named parameters with positional ones for backends not supporting named params (MySQL/MariaDB)
var namedParamRegex = regexp.MustCompile(`@[a-zA-Z0-9_]+`)

func adaptQuery(q string) string {
	if dbBackend == "mariadb" || dbBackend == "mysql" {
		return namedParamRegex.ReplaceAllString(q, "?")
	}
	return q
}

//---------------------------------------------------------------------
// Daten-Structs – identisch zu vorher, damit main.go unverändert bleibt
//---------------------------------------------------------------------

type User struct {
	ID           int
	Stampkey     string
	Name         string
	Email        string
	Position     string
	DepartmentID int
	TenantID     int // Added for multi-tenant support
}

type Activity struct {
	ID       int
	Status   string
	Work     int
	Comment  string
	TenantID int // Added for multi-tenant support
}

type Department struct {
	ID       int
	Name     string
	TenantID int // Added for multi-tenant support
}

//---------------------------------------------------------------------
// CRUD-Funktionen
//---------------------------------------------------------------------

// ----------- SELECT-Listen ------------------------------------------

func getUsers() []User {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(fmt.Sprintf("SELECT id, name, email, position, department_id, stampkey FROM %s", tbl("users")))
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Position, &u.DepartmentID, &u.Stampkey); err != nil {
			log.Fatal(err)
		}
		list = append(list, u)
	}
	return list
}

func getActivities() []Activity {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(fmt.Sprintf("SELECT id, status, work, comment FROM %s", tbl("type")))
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var list []Activity
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.Status, &a.Work, &a.Comment); err != nil {
			log.Fatal(err)
		}
		list = append(list, a)
	}
	return list
}

func getDepartments() []Department {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(fmt.Sprintf("SELECT id, name FROM %s", tbl("departments")))
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var list []Department
	for rows.Next() {
		var d Department
		if err := rows.Scan(&d.ID, &d.Name); err != nil {
			log.Fatal(err)
		}
		list = append(list, d)
	}
	return list
}

func getEntries() []Entry {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(fmt.Sprintf("SELECT id, user_id, type_id, date FROM %s", tbl("entries")))
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var list []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.UserID, &e.ActivityID, &e.Date); err != nil {
			log.Fatal(err)
		}
		list = append(list, e)
	}
	return list
}

// ----------- SELECT-Einzelne ----------------------------------------

func getUser(id string) User {
	db := getDB()
	defer db.Close()

	query := adaptQuery(fmt.Sprintf("SELECT id, name, stampkey, email, position, department_id FROM %s WHERE id=@id", tbl("users")))
	var u User
	if err := db.QueryRow(query, sql.Named("id", id)).
		Scan(&u.ID, &u.Name, &u.Stampkey, &u.Email, &u.Position, &u.DepartmentID); err != nil {
		log.Fatal(err)
	}
	return u
}

func getAllUsers() []User {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("SELECT id, name, stampkey, email, position, department_id FROM %s", tbl("users"))
	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Stampkey, &u.Email, &u.Position, &u.DepartmentID); err != nil {
			log.Fatal(err)
		}
		users = append(users, u)
	}
	return users
}

func getAllActivities() []Activity {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("SELECT id, status, work, comment FROM %s", tbl("type"))
	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var activities []Activity
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.Status, &a.Work, &a.Comment); err != nil {
			log.Fatal(err)
		}
		activities = append(activities, a)
	}
	return activities
}

func getActivity(id string) Activity {
	db := getDB()
	defer db.Close()

	query := adaptQuery(fmt.Sprintf("SELECT id, status, work, comment FROM %s WHERE id=@id", tbl("type")))
	var a Activity
	if err := db.QueryRow(query, sql.Named("id", id)).
		Scan(&a.ID, &a.Status, &a.Work, &a.Comment); err != nil {
		log.Fatal(err)
	}
	return a
}

func getDepartment(id string) Department {
	db := getDB()
	defer db.Close()

	query := adaptQuery(fmt.Sprintf("SELECT id, name FROM %s WHERE id=@id", tbl("departments")))
	var d Department
	if err := db.QueryRow(query, sql.Named("id", id)).
		Scan(&d.ID, &d.Name); err != nil {
		log.Fatal(err)
	}
	return d
}

func getUserIDFromStampKey(stampKey string) string {
	db := getDB()
	defer db.Close()

	query := adaptQuery(fmt.Sprintf("SELECT id FROM %s WHERE stampkey=@sk", tbl("users")))
	var id string
	if err := db.QueryRow(query, sql.Named("sk", stampKey)).Scan(&id); err != nil {
		// kein fatal – kann vorkommen, wenn Karte unbekannt
		return ""
	}
	return id
}

func getUserIDByEmail(email string) (string, error) {
	db := getDB()
	defer db.Close()

	query := adaptQuery(fmt.Sprintf("SELECT id FROM %s WHERE email=@eml", tbl("users")))
	var id string
	if err := db.QueryRow(query, sql.Named("eml", email)).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

// ----------- INSERT --------------------------------------------------

func createUniqueStampKey() int {
	db := getDB()
	defer db.Close()

	// Generiere einen eindeutigen Stampkey (hier einfach eine Zufallszahl)
	// In der Praxis sollte dies robuster sein, z.B. durch UUIDs oder andere Mechanismen
	for {
		//stampKey := time.Now().UnixNano() + int64(os.Getpid())
		// stampkey sollte eindeutig sein und zwischen 100000 und 999999999999 liegen
		stampKey := time.Now().UnixNano()%900000000000 + 100000000000 // 12-stellig

		// Überprüfen, ob der Stampkey bereits existiert
		query := adaptQuery(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE stampkey=@sk", tbl("users")))
		var count int
		if err := db.QueryRow(query, sql.Named("sk", stampKey)).Scan(&count); err != nil {
			log.Fatal(err)
		}
		if count == 0 {
			return int(stampKey)
		}
	}
}

func createUser(name, stampkey, email, position, departmentID string) {
	db := getDB()
	defer db.Close()

	// Überprüfen, ob der Stampkey bereits existiert
	if stampkey == "" {
		// Generiere einen neuen eindeutigen Stampkey
		stampkey = strconv.Itoa(createUniqueStampKey())
	} else {
		// Überprüfen, ob der Stampkey bereits existiert
		query := adaptQuery(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE stampkey=@sk", tbl("users")))
		var count int
		if err := db.QueryRow(query, sql.Named("sk", stampkey)).Scan(&count); err != nil {
			log.Fatal(err)
		}

		if count > 0 {
			log.Fatalf("Stampkey %s already exists. Please use a different one.", stampkey)
		}
	}

	dept, _ := strconv.Atoi(departmentID)
	query := adaptQuery(fmt.Sprintf(`INSERT INTO %s (name, stampkey, email, position, department_id)
						   VALUES (@name,@sk,@mail,@pos,@dept)`, tbl("users")))
	_, err := db.Exec(query,
		sql.Named("name", name),
		sql.Named("sk", stampkey),
		sql.Named("mail", email),
		sql.Named("pos", position),
		sql.Named("dept", dept),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func createActivity(status, work, comment string) {
	db := getDB()
	defer db.Close()

	workInt, _ := strconv.Atoi(work)
	query := adaptQuery(fmt.Sprintf(`INSERT INTO %s (status, work, comment)
						   VALUES (@status,@work,@comment)`, tbl("type")))
	_, err := db.Exec(query,
		sql.Named("status", status),
		sql.Named("work", workInt),
		sql.Named("comment", comment),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func createDepartment(name string) {
	db := getDB()
	defer db.Close()

	query := adaptQuery(fmt.Sprintf("INSERT INTO %s (name) VALUES (@name)", tbl("departments")))
	if _, err := db.Exec(query, sql.Named("name", name)); err != nil {
		log.Fatal(err)
	}
}

// createEntry creates a new time entry for a user
func createEntry(userID, activityID string, entrydate time.Time) {
	db := getDB()
	defer db.Close()

	query := adaptQuery(fmt.Sprintf(`INSERT INTO %s (user_id, type_id, date)
						VALUES (@uid, @aid, @date)`, tbl("entries")))
	_, err := db.Exec(query,
		sql.Named("uid", userID),
		sql.Named("aid", activityID),
		sql.Named("date", entrydate),
	)
	if err != nil {
		log.Fatal(err)
	}
}

// ----------- UPDATE --------------------------------------------------

func updateUser(id, name, stampkey, email, position, departmentID string) {
	db := getDB()
	defer db.Close()

	dept, _ := strconv.Atoi(departmentID)
	query := adaptQuery(fmt.Sprintf(`UPDATE %s
	                      SET name=@name, stampkey=@sk, email=@mail, position=@pos, department_id=@dept
					  WHERE id=@id`, tbl("users")))
	_, err := db.Exec(query,
		sql.Named("name", name),
		sql.Named("sk", stampkey),
		sql.Named("mail", email),
		sql.Named("pos", position),
		sql.Named("dept", dept),
		sql.Named("id", id),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func updateActivity(id, status, work, comment string) {
	db := getDB()
	defer db.Close()

	workInt, _ := strconv.Atoi(work)
	query := adaptQuery(fmt.Sprintf(`UPDATE %s
	                      SET status=@status, work=@work, comment=@comment
					  WHERE id=@id`, tbl("type")))
	_, err := db.Exec(query,
		sql.Named("status", status),
		sql.Named("work", workInt),
		sql.Named("comment", comment),
		sql.Named("id", id),
	)
	if err != nil {
		log.Fatal(err)
	}
}

//---------------------------------------------------------------------
// Sichten für Auswertungen
//---------------------------------------------------------------------

func getWorkHoursData() []WorkHoursData {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(fmt.Sprintf("SELECT user_name, work_date, work_hours FROM %s", tbl("work_hours")))
	if err != nil {
		log.Printf("Query work_hours failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []WorkHoursData
	for rows.Next() {
		var w WorkHoursData
		if err := rows.Scan(&w.UserName, &w.WorkDate, &w.WorkHours); err != nil {
			log.Fatal(err)
		}
		list = append(list, w)
	}
	return list
}

func getCurrentStatusData() []CurrentStatusData {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(fmt.Sprintf("SELECT user_name, status, date FROM %s", tbl("current_status")))
	if err != nil {
		log.Printf("Query current_status failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []CurrentStatusData
	for rows.Next() {
		var c CurrentStatusData
		if err := rows.Scan(&c.UserName, &c.Status, &c.Date); err != nil {
			log.Fatal(err)
		}
		list = append(list, c)
	}
	return list
}
