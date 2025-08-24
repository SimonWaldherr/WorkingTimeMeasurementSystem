package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"runtime"
	"sync"

	_ "github.com/denisenkom/go-mssqldb"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

//---------------------------------------------------------------------
// eingebettete Schema-Dateien
//---------------------------------------------------------------------

//go:embed timetrack_init.sql
var embeddedSQLiteSchema string

//go:embed timetrack_init.mssql.sql
var embeddedMSSQLSchema string

//---------------------------------------------------------------------
// globale Konfiguration
//---------------------------------------------------------------------

var (
	dbBackend            string // "sqlite" | "mssql"
	sqlitePath           string
	mssqlServer, mssqlDB string
	mssqlUser, mssqlPass string
	mssqlPort            int
)

// request-bound host mapping for SQLite multi-tenant support
var currentHostByGID sync.Map // gid -> host
var initializedDBs sync.Map   // sqlite dsn/path -> bool

// SetRequestHost binds the current goroutine to a host for DB selection
func SetRequestHost(host string) {
	currentHostByGID.Store(getGID(), host)
}

// ClearRequestHost clears the host binding for the current goroutine
func ClearRequestHost() {
	currentHostByGID.Delete(getGID())
}

func getGID() int64 {
	// Hacky but sufficient: parse goroutine id from runtime.Stack
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// Stack format: "goroutine 12345 [running]:\n"
	s := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))
	if len(s) > 0 {
		if id, err := strconv.ParseInt(s[0], 10, 64); err == nil {
			return id
		}
	}
	return 0
}

func resolveSQLitePath() string {
	// Prefer request-bound host-specific DB path when available
	if v, ok := currentHostByGID.Load(getGID()); ok {
		host := fmt.Sprintf("%v", v)
		// sanitize host for filesystem
		safe := strings.ToLower(host)
		safe = strings.ReplaceAll(safe, "/", "-")
		// ensure database dir exists
		dir := "database"
		_ = os.MkdirAll(dir, 0o755)
		return filepath.Join(dir, "time_tracking."+safe+".db")
	}
	// fallback to configured path
	return sqlitePath
}

// EnsureSchemaCurrent ensures that for the current DB target (considering
// the request-bound host for SQLite) the schema exists. It runs at most once
// per SQLite file path.
func EnsureSchemaCurrent() {
	if dbBackend != "sqlite" {
		return
	}
	path := resolveSQLitePath()
	if _, done := initializedDBs.Load(path); done {
		return
	}
	// Try to run schema creation idempotently for this path
	log.Printf("[DB] Ensuring schema for SQLite at %s", path)
	execBatches(embeddedSQLiteSchema, ";\n")
	ensureUserPasswordColumn()
	ensureUserRoleColumn()
	initializedDBs.Store(path, true)
}

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
	log.Printf("[DB] Backend=mssql server=%s db=%s user=%s port=%d", mssqlServer, mssqlDB, mssqlUser, mssqlPort)
	default: // sqlite
		sqlitePath = getenv("SQLITE_PATH", "time_tracking.db")
	log.Printf("[DB] Backend=sqlite defaultPath=%s (will switch per-host if set)", sqlitePath)
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
	default: // sqlite
		driver = "sqlite3"
		dsn = resolveSQLitePath()
		log.Printf("[DB] Opening SQLite dsn=%s", dsn)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
        log.Fatalf("[DB] Open failed driver=%s dsn=%s err=%v", driver, dsn, err)
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

//---------------------------------------------------------------------
// Schema anlegen
//---------------------------------------------------------------------

func createDatabaseAndTables() {
	switch dbBackend {
	case "sqlite":
		execBatches(embeddedSQLiteSchema, ";\n")
	ensureUserPasswordColumn()
	ensureUserRoleColumn()
	case "mssql":
		if os.Getenv("DB_AUTO_MIGRATE") == "1" {
			//execBatches(embeddedMSSQLSchema, "\nGO")
		}
	ensureUserPasswordColumn()
	ensureUserRoleColumn()
	}
}

// ensureUserPasswordColumn adds the password column if it does not exist
func ensureUserPasswordColumn() {
	db := getDB()
	defer db.Close()
	switch dbBackend {
	case "sqlite":
		rows, err := db.Query("PRAGMA table_info(users)")
		if err != nil {
			log.Printf("PRAGMA table_info failed: %v", err)
			return
		}
		defer rows.Close()
		hasPwd := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
				if strings.EqualFold(name, "password") { hasPwd = true; break }
			}
		}
		if !hasPwd {
			if _, err := db.Exec("ALTER TABLE users ADD COLUMN password TEXT"); err != nil {
				log.Printf("add users.password failed: %v", err)
			}
		}
	case "mssql":
		var exists int
		err := db.QueryRow("SELECT 1 FROM sys.columns WHERE Name = 'password' AND Object_ID = Object_ID('dbo.users')").Scan(&exists)
		if err == sql.ErrNoRows {
			if _, err2 := db.Exec("ALTER TABLE dbo.users ADD password NVARCHAR(255) NULL"); err2 != nil {
				log.Printf("add users.password failed: %v", err2)
			}
		}
	}
}

// ensureUserRoleColumn adds the role column if missing
func ensureUserRoleColumn() {
	db := getDB()
	defer db.Close()
	switch dbBackend {
	case "sqlite":
		rows, err := db.Query("PRAGMA table_info(users)")
		if err != nil { return }
		defer rows.Close()
		has := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
				if strings.EqualFold(name, "role") { has = true; break }
			}
		}
		if !has {
			_, _ = db.Exec("ALTER TABLE users ADD COLUMN role TEXT DEFAULT 'user'")
		}
	case "mssql":
		var exists int
		err := db.QueryRow("SELECT 1 FROM sys.columns WHERE Name = 'role' AND Object_ID = Object_ID('dbo.users')").Scan(&exists)
		if err == sql.ErrNoRows {
			_, _ = db.Exec("ALTER TABLE dbo.users ADD role NVARCHAR(50) NULL DEFAULT 'user'")
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
			log.Printf("[DB] Schema exec failed: %v; stmt: %s", err, stmt)
		}
	}
}

//---------------------------------------------------------------------
// Daten-Structs – identisch zu vorher, damit main.go unverändert bleibt
//---------------------------------------------------------------------

type User struct {
	ID           int
	Stampkey     string
	Name         string
	Email        string
	Password     string
	Role         string
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

//---------------------------------------------------------------------
// CRUD-Funktionen
//---------------------------------------------------------------------

// ----------- SELECT-Listen ------------------------------------------

func getUsers() []User {
	db := getDB()
	defer db.Close()

	rows, err := db.Query(fmt.Sprintf("SELECT id, name, email, COALESCE(password,''), COALESCE(role,'user'), position, department_id, stampkey FROM %s", tbl("users")))
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var u User
	if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Password, &u.Role, &u.Position, &u.DepartmentID, &u.Stampkey); err != nil {
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

	query := fmt.Sprintf("SELECT id, name, stampkey, email, COALESCE(password,''), COALESCE(role,'user'), position, department_id FROM %s WHERE id=@id", tbl("users"))
	var u User
	if err := db.QueryRow(query, sql.Named("id", id)).
	Scan(&u.ID, &u.Name, &u.Stampkey, &u.Email, &u.Password, &u.Role, &u.Position, &u.DepartmentID); err != nil {
		log.Fatal(err)
	}
	return u
}

func getAllUsers() []User {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("SELECT id, name, stampkey, email, COALESCE(password,''), COALESCE(role,'user'), position, department_id FROM %s", tbl("users"))
	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
	if err := rows.Scan(&u.ID, &u.Name, &u.Stampkey, &u.Email, &u.Password, &u.Role, &u.Position, &u.DepartmentID); err != nil {
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

	query := fmt.Sprintf("SELECT id, status, work, comment FROM %s WHERE id=@id", tbl("type"))
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

	query := fmt.Sprintf("SELECT id, name FROM %s WHERE id=@id", tbl("departments"))
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

	query := fmt.Sprintf("SELECT id FROM %s WHERE stampkey=@sk", tbl("users"))
	var id string
	if err := db.QueryRow(query, sql.Named("sk", stampKey)).Scan(&id); err != nil {
		// kein fatal – kann vorkommen, wenn Karte unbekannt
		return ""
	}
	return id
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
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE stampkey=@sk", tbl("users"))
		var count int
		if err := db.QueryRow(query, sql.Named("sk", stampKey)).Scan(&count); err != nil {
			log.Fatal(err)
		}
		if count == 0 {
			return int(stampKey)
		}
	}
}

func createUser(name, stampkey, email, password, role, position, departmentID string) {
	db := getDB()
	defer db.Close()

	// Überprüfen, ob der Stampkey bereits existiert
	if stampkey == "" {
		// Generiere einen neuen eindeutigen Stampkey
		stampkey = strconv.Itoa(createUniqueStampKey())
	} else {
		// Überprüfen, ob der Stampkey bereits existiert
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE stampkey=@sk", tbl("users"))
		var count int
		if err := db.QueryRow(query, sql.Named("sk", stampkey)).Scan(&count); err != nil {
			log.Fatal(err)
		}

		if count > 0 {
			log.Fatalf("Stampkey %s already exists. Please use a different one.", stampkey)
		}
	}

	dept, _ := strconv.Atoi(departmentID)
	// hash password if provided
	var hashed string
	if password != "" {
		b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("hash password failed: %v", err)
		} else {
			hashed = string(b)
		}
	}
	query := fmt.Sprintf(`INSERT INTO %s (name, stampkey, email, password, role, position, department_id)
						   VALUES (@name,@sk,@mail,@pwd,@role,@pos,@dept)`, tbl("users"))
	_, err := db.Exec(query,
		sql.Named("name", name),
		sql.Named("sk", stampkey),
		sql.Named("mail", email),
		sql.Named("pwd", hashed),
		sql.Named("role", role),
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
	query := fmt.Sprintf(`INSERT INTO %s (status, work, comment)
	                       VALUES (@status,@work,@comment)`, tbl("type"))
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

	query := fmt.Sprintf("INSERT INTO %s (name) VALUES (@name)", tbl("departments"))
	if _, err := db.Exec(query, sql.Named("name", name)); err != nil {
		log.Fatal(err)
	}
}

// createEntry creates a new time entry for a user
func createEntry(userID, activityID string, entrydate time.Time) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`INSERT INTO %s (user_id, type_id, date)
						VALUES (@uid, @aid, @date)`, tbl("entries"))
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

func updateUser(id, name, stampkey, email, password, role, position, departmentID string) {
	db := getDB()
	defer db.Close()

	dept, _ := strconv.Atoi(departmentID)
	if password != "" {
		b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		var hashed string
		if err != nil {
			log.Printf("hash password failed: %v", err)
		} else {
			hashed = string(b)
		}
	query := fmt.Sprintf(`UPDATE %s
			  SET name=@name, stampkey=@sk, email=@mail, password=@pwd, role=@role, position=@pos, department_id=@dept
						  WHERE id=@id`, tbl("users"))
		_, err = db.Exec(query,
			sql.Named("name", name),
			sql.Named("sk", stampkey),
			sql.Named("mail", email),
			sql.Named("pwd", hashed),
	    sql.Named("role", role),
			sql.Named("pos", position),
			sql.Named("dept", dept),
			sql.Named("id", id),
		)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	query := fmt.Sprintf(`UPDATE %s
						  SET name=@name, stampkey=@sk, email=@mail, role=@role, position=@pos, department_id=@dept
						  WHERE id=@id`, tbl("users"))
	_, err := db.Exec(query,
		sql.Named("name", name),
		sql.Named("sk", stampkey),
		sql.Named("mail", email),
		sql.Named("role", role),
		sql.Named("pos", position),
		sql.Named("dept", dept),
		sql.Named("id", id),
	)
	if err != nil {
		log.Fatal(err)
	}
}

// Lookup user by email
func getUserByEmail(email string) (User, bool) {
	db := getDB()
	defer db.Close()
	query := fmt.Sprintf("SELECT id, name, email, COALESCE(password,''), COALESCE(role,'user'), stampkey, position, COALESCE(department_id,0) FROM %s WHERE email=@mail", tbl("users"))
	var u User
	if err := db.QueryRow(query, sql.Named("mail", email)).Scan(&u.ID, &u.Name, &u.Email, &u.Password, &u.Role, &u.Stampkey, &u.Position, &u.DepartmentID); err != nil {
		return User{}, false
	}
	return u, true
}

func updateActivity(id, status, work, comment string) {
	db := getDB()
	defer db.Close()

	workInt, _ := strconv.Atoi(work)
	query := fmt.Sprintf(`UPDATE %s
	                      SET status=@status, work=@work, comment=@comment
	                      WHERE id=@id`, tbl("type"))
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

// Additional CRUD functions for editing
func updateDepartment(id, name string) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`UPDATE %s SET name=@name WHERE id=@id`, tbl("departments"))
	_, err := db.Exec(query,
		sql.Named("name", name),
		sql.Named("id", id),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func updateEntry(id, userID, activityID, date, comment string) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`UPDATE %s
	                      SET user_id=@uid, type_id=@aid, date=@date, comment=@comment
	                      WHERE id=@id`, tbl("entries"))
	_, err := db.Exec(query,
		sql.Named("uid", userID),
		sql.Named("aid", activityID),
		sql.Named("date", date),
		sql.Named("comment", comment),
		sql.Named("id", id),
	)
	if err != nil {
		log.Fatal(err)
	}
}

func getEntry(id string) EntryDetail {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
		SELECT 
			e.id,
			u.name as user_name,
			COALESCE(d.name, 'No Department') as department,
			t.status as activity,
			e.date,
			e.date as start_time,
			COALESCE(
				(SELECT MIN(next_e.date) FROM %s next_e 
				 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
				datetime('now')
			) as end_time,
			COALESCE(
				(JULIANDAY(
					COALESCE(
						(SELECT MIN(next_e.date) FROM %s next_e 
						 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
						datetime('now')
					)
				) - JULIANDAY(e.date)) * 24, 0
			) as duration,
			COALESCE(e.comment, '') as comment
		FROM %s e
		JOIN %s u ON e.user_id = u.id
		LEFT JOIN %s d ON u.department_id = d.id
		JOIN %s t ON e.type_id = t.id
		WHERE e.id = @id
	`, tbl("entries"), tbl("entries"), tbl("entries"), tbl("users"), tbl("departments"), tbl("type"))

	var e EntryDetail
	if err := db.QueryRow(query, sql.Named("id", id)).
		Scan(&e.ID, &e.UserName, &e.Department, &e.Activity, &e.Date, &e.Start, &e.End, &e.Duration, &e.Comment); err != nil {
		log.Printf("Get entry failed: %v", err)
		return EntryDetail{}
	}
	return e
}

// Delete functions
func deleteEntry(id string) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("DELETE FROM %s WHERE id=@id", tbl("entries"))
	_, err := db.Exec(query, sql.Named("id", id))
	if err != nil {
		log.Fatal(err)
	}
}

func deleteActivity(id string) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("DELETE FROM %s WHERE id=@id", tbl("type"))
	_, err := db.Exec(query, sql.Named("id", id))
	if err != nil {
		log.Fatal(err)
	}
}

func deleteDepartment(id string) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("DELETE FROM %s WHERE id=@id", tbl("departments"))
	_, err := db.Exec(query, sql.Named("id", id))
	if err != nil {
		log.Fatal(err)
	}
}

func deleteUser(id string) {
	db := getDB()
	defer db.Close()

	// First delete all entries for this user
	query := fmt.Sprintf("DELETE FROM %s WHERE user_id=@id", tbl("entries"))
	_, err := db.Exec(query, sql.Named("id", id))
	if err != nil {
		log.Fatal(err)
	}

	// Then delete the user
	query = fmt.Sprintf("DELETE FROM %s WHERE id=@id", tbl("users"))
	_, err = db.Exec(query, sql.Named("id", id))
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

// Enhanced statistics data structures
type DepartmentSummary struct {
	DepartmentName  string
	TotalUsers      int
	TotalHours      float64
	AvgHoursPerUser float64
}

type TimeTrackingTrend struct {
	Date         string
	TotalHours   float64
	ActiveUsers  int
	WorkEntries  int
	BreakEntries int
}

type UserActivitySummary struct {
	UserName        string
	Department      string
	TotalWorkHours  float64
	TotalBreakHours float64
	LastActivity    string
	Status          string
}

type EntryDetail struct {
	ID         int
	UserName   string
	Department string
	Activity   string
	Date       string
	Start      string
	End        string
	Duration   float64
	Comment    string
}

// Enhanced statistics functions
func getDepartmentSummary() []DepartmentSummary {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
		SELECT 
			d.name as department_name,
			COUNT(DISTINCT u.id) as total_users,
			COALESCE(SUM(wh.work_hours), 0) as total_hours,
			CASE 
				WHEN COUNT(DISTINCT u.id) > 0 
				THEN COALESCE(SUM(wh.work_hours), 0) / COUNT(DISTINCT u.id)
				ELSE 0 
			END as avg_hours_per_user
		FROM %s d
		LEFT JOIN %s u ON d.id = u.department_id
		LEFT JOIN %s wh ON u.name = wh.user_name
		GROUP BY d.id, d.name
		ORDER BY total_hours DESC
	`, tbl("departments"), tbl("users"), tbl("work_hours"))

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Query department summary failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []DepartmentSummary
	for rows.Next() {
		var d DepartmentSummary
		if err := rows.Scan(&d.DepartmentName, &d.TotalUsers, &d.TotalHours, &d.AvgHoursPerUser); err != nil {
			log.Printf("Scan department summary failed: %v", err)
			continue
		}
		list = append(list, d)
	}
	return list
}

func getTimeTrackingTrends(days int) []TimeTrackingTrend {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
		WITH dates AS (
			SELECT date('now', '-%d days') as date
			UNION ALL
			SELECT date(date, '+1 day') FROM dates WHERE date < date('now')
		),
		daily_stats AS (
			SELECT 
				d.date as work_date,
				COUNT(CASE WHEN t.work = 1 THEN 1 END) as work_entries,
				COUNT(CASE WHEN t.work = 0 THEN 1 END) as break_entries,
				COUNT(DISTINCT e.user_id) as active_users,
				COALESCE(SUM(
					CASE WHEN t.work = 1 THEN 
						(JULIANDAY(
							COALESCE(
								(SELECT MIN(next_e.date) FROM %s next_e 
								 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
								datetime('now')
							)
						) - JULIANDAY(e.date)) * 24
					ELSE 0 END
				), 0) as total_hours
			FROM dates d
			LEFT JOIN %s e ON DATE(e.date) = d.date
			LEFT JOIN %s t ON e.type_id = t.id
			GROUP BY d.date
		)
		SELECT 
			work_date,
			ROUND(total_hours, 2) as total_hours,
			active_users,
			work_entries,
			break_entries
		FROM daily_stats
		ORDER BY work_date DESC
	`, days, tbl("entries"), tbl("entries"), tbl("type"))

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Query time tracking trends failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []TimeTrackingTrend
	for rows.Next() {
		var t TimeTrackingTrend
		var date sql.NullString
		if err := rows.Scan(&date, &t.TotalHours, &t.ActiveUsers, &t.WorkEntries, &t.BreakEntries); err != nil {
			log.Printf("Scan time tracking trend failed: %v", err)
			continue
		}
		if date.Valid {
			t.Date = date.String
		} else {
			t.Date = ""
		}
		list = append(list, t)
	}
	return list
}

func getUserActivitySummary() []UserActivitySummary {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
		SELECT 
			u.name as user_name,
			COALESCE(d.name, 'No Department') as department,
			COALESCE(SUM(CASE WHEN t.work = 1 THEN 
				(JULIANDAY(
					COALESCE(
						(SELECT MIN(next_e.date) FROM %s next_e 
						 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
						datetime('now')
					)
				) - JULIANDAY(e.date)) * 24
			ELSE 0 END), 0) as total_work_hours,
			COALESCE(SUM(CASE WHEN t.work = 0 THEN 
				(JULIANDAY(
					COALESCE(
						(SELECT MIN(next_e.date) FROM %s next_e 
						 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
						datetime('now')
					)
				) - JULIANDAY(e.date)) * 24
			ELSE 0 END), 0) as total_break_hours,
			MAX(e.date) as last_activity,
			(SELECT t2.status FROM %s e2 
			 JOIN %s t2 ON e2.type_id = t2.id 
			 WHERE e2.user_id = u.id 
			 ORDER BY e2.date DESC LIMIT 1) as current_status
		FROM %s u
		LEFT JOIN %s d ON u.department_id = d.id
		LEFT JOIN %s e ON u.id = e.user_id
		LEFT JOIN %s t ON e.type_id = t.id
		GROUP BY u.id, u.name, d.name
		ORDER BY total_work_hours DESC
	`, tbl("entries"), tbl("entries"), tbl("entries"), tbl("type"), tbl("users"), tbl("departments"), tbl("entries"), tbl("type"))

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Query user activity summary failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []UserActivitySummary
	for rows.Next() {
		var u UserActivitySummary
		if err := rows.Scan(&u.UserName, &u.Department, &u.TotalWorkHours, &u.TotalBreakHours, &u.LastActivity, &u.Status); err != nil {
			log.Printf("Scan user activity summary failed: %v", err)
			continue
		}
		list = append(list, u)
	}
	return list
}

func getEntriesWithDetails() []EntryDetail {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
		SELECT 
			e.id,
			u.name as user_name,
			COALESCE(d.name, 'No Department') as department,
			t.status as activity,
			e.date,
			e.date as start_time,
			COALESCE(
				(SELECT MIN(next_e.date) FROM %s next_e 
				 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
				datetime('now')
			) as end_time,
			COALESCE(
				(JULIANDAY(
					COALESCE(
						(SELECT MIN(next_e.date) FROM %s next_e 
						 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
						datetime('now')
					)
				) - JULIANDAY(e.date)) * 24, 0
			) as duration,
			COALESCE(e.comment, '') as comment
		FROM %s e
		JOIN %s u ON e.user_id = u.id
		LEFT JOIN %s d ON u.department_id = d.id
		JOIN %s t ON e.type_id = t.id
		ORDER BY e.date DESC
		LIMIT 1000
	`, tbl("entries"), tbl("entries"), tbl("entries"), tbl("users"), tbl("departments"), tbl("type"))

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Query entries with details failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []EntryDetail
	for rows.Next() {
		var e EntryDetail
		if err := rows.Scan(&e.ID, &e.UserName, &e.Department, &e.Activity, &e.Date, &e.Start, &e.End, &e.Duration, &e.Comment); err != nil {
			log.Printf("Scan entry detail failed: %v", err)
			continue
		}
		list = append(list, e)
	}
	return list
}
