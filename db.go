package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
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
		// ensure tenant dir exists: tenant/<host>
		dir := filepath.Join("tenant", safe)
		_ = os.MkdirAll(dir, 0o755)
		return filepath.Join(dir, "time_tracking.db")
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
	ensureUserAutoCheckoutColumn()
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
		driver = "sqlite"
		dsn = resolveSQLitePath()
		log.Printf("[DB] Opening SQLite dsn=%s", dsn)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		// don't crash the server; return a dummy DB that will fail later
		log.Printf("[DB] Open failed driver=%s dsn=%s err=%v", driver, dsn, err)
		return db
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
		ensureUserAutoCheckoutColumn()
	case "mssql":
		if os.Getenv("DB_AUTO_MIGRATE") == "1" {
			//execBatches(embeddedMSSQLSchema, "\nGO")
		}
		ensureUserPasswordColumn()
		ensureUserRoleColumn()
		ensureUserAutoCheckoutColumn()
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
				if strings.EqualFold(name, "password") {
					hasPwd = true
					break
				}
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
		if err != nil {
			return
		}
		defer rows.Close()
		has := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
				if strings.EqualFold(name, "role") {
					has = true
					break
				}
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

// ensureUserAutoCheckoutColumn adds the auto_checkout_midnight column if missing
func ensureUserAutoCheckoutColumn() {
	db := getDB()
	defer db.Close()
	switch dbBackend {
	case "sqlite":
		rows, err := db.Query("PRAGMA table_info(users)")
		if err != nil {
			return
		}
		defer rows.Close()
		has := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
				if strings.EqualFold(name, "auto_checkout_midnight") {
					has = true
					break
				}
			}
		}
		if !has {
			_, _ = db.Exec("ALTER TABLE users ADD COLUMN auto_checkout_midnight INTEGER DEFAULT 0")
		}
	case "mssql":
		var exists int
		err := db.QueryRow("SELECT 1 FROM sys.columns WHERE Name = 'auto_checkout_midnight' AND Object_ID = Object_ID('dbo.users')").Scan(&exists)
		if err == sql.ErrNoRows {
			_, _ = db.Exec("ALTER TABLE dbo.users ADD auto_checkout_midnight INT NOT NULL DEFAULT 0")
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
	ID                   int
	Stampkey             string
	Name                 string
	Email                string
	Password             string
	Role                 string
	Position             string
	DepartmentID         int
	AutoCheckoutMidnight int
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

	rows, err := db.Query(fmt.Sprintf("SELECT id, name, email, COALESCE(password,''), COALESCE(role,'user'), position, department_id, stampkey, COALESCE(auto_checkout_midnight,0) FROM %s", tbl("users")))
	if err != nil {
		log.Printf("getUsers query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Password, &u.Role, &u.Position, &u.DepartmentID, &u.Stampkey, &u.AutoCheckoutMidnight); err != nil {
			log.Printf("getUsers scan failed: %v", err)
			continue
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
		log.Printf("getActivities query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []Activity
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.Status, &a.Work, &a.Comment); err != nil {
			log.Printf("getActivities scan failed: %v", err)
			continue
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
		log.Printf("getDepartments query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []Department
	for rows.Next() {
		var d Department
		if err := rows.Scan(&d.ID, &d.Name); err != nil {
			log.Printf("getDepartments scan failed: %v", err)
			continue
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
		log.Printf("getEntries query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.UserID, &e.ActivityID, &e.Date); err != nil {
			log.Printf("getEntries scan failed: %v", err)
			continue
		}
		list = append(list, e)
	}
	return list
}

// ----------- SELECT-Einzelne ----------------------------------------

func getUser(id string) User {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("SELECT id, name, stampkey, email, COALESCE(password,''), COALESCE(role,'user'), position, department_id, COALESCE(auto_checkout_midnight,0) FROM %s WHERE id=@id", tbl("users"))
	var u User
	if err := db.QueryRow(query, sql.Named("id", id)).
		Scan(&u.ID, &u.Name, &u.Stampkey, &u.Email, &u.Password, &u.Role, &u.Position, &u.DepartmentID, &u.AutoCheckoutMidnight); err != nil {
		log.Printf("getUser failed: %v", err)
		return User{}
	}
	return u
}

func getAllUsers() []User {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("SELECT id, name, stampkey, email, COALESCE(password,''), COALESCE(role,'user'), position, department_id, COALESCE(auto_checkout_midnight,0) FROM %s", tbl("users"))
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("getAllUsers query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Stampkey, &u.Email, &u.Password, &u.Role, &u.Position, &u.DepartmentID, &u.AutoCheckoutMidnight); err != nil {
			log.Printf("getAllUsers scan failed: %v", err)
			continue
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
		log.Printf("getAllActivities query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var activities []Activity
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.Status, &a.Work, &a.Comment); err != nil {
			log.Printf("getAllActivities scan failed: %v", err)
			continue
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
		log.Printf("getActivity failed: %v", err)
		return Activity{}
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
		log.Printf("getDepartment failed: %v", err)
		return Department{}
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
			log.Printf("createUniqueStampKey check failed: %v", err)
			continue
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
			log.Printf("createUser check sk failed: %v", err)
			count = 0
		}

		if count > 0 {
			log.Printf("Stampkey %s already exists. Please use a different one.", stampkey)
			return
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
		log.Printf("createUser insert failed: %v", err)
	}
}

// setUserAutoCheckout updates the per-user auto checkout flag (0/1)
func setUserAutoCheckout(id string, enabled bool) {
	db := getDB()
	defer db.Close()
	val := 0
	if enabled {
		val = 1
	}
	query := fmt.Sprintf("UPDATE %s SET auto_checkout_midnight=@auto WHERE id=@id", tbl("users"))
	if _, err := db.Exec(query, sql.Named("auto", val), sql.Named("id", id)); err != nil {
		log.Printf("update auto_checkout_midnight failed: %v", err)
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
		log.Printf("updateDepartment failed: %v", err)
	}
}

func createDepartment(name string) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("INSERT INTO %s (name) VALUES (@name)", tbl("departments"))
	if _, err := db.Exec(query, sql.Named("name", name)); err != nil {
		log.Printf("createDepartment failed: %v", err)
	}
}

// createEntry creates a new time entry for a user
func createEntry(userID, activityID string, entrydate time.Time) {
	db := getDB()
	defer db.Close()

	// Ensure midnight auto-checkout if enabled and last working entry is on a previous day
	ensureMidnightAutoCheckoutWithDB(db, atoiDefault(userID, 0), entrydate)

	query := fmt.Sprintf(`INSERT INTO %s (user_id, type_id, date)
                            VALUES (@uid, @aid, @date)`, tbl("entries"))
	_, err := db.Exec(query,
		sql.Named("uid", userID),
		sql.Named("aid", activityID),
		sql.Named("date", entrydate),
	)
	if err != nil {
		log.Printf("createEntry failed: %v", err)
	}
}

// ensureMidnightAutoCheckoutWithDB inserts a non-work entry at 23:59:59 of the day of the
// user's last working entry if auto checkout is enabled and the last entry is from a previous day.
func ensureMidnightAutoCheckoutWithDB(db *sql.DB, userID int, now time.Time) {
	if userID <= 0 {
		return
	}
	var auto int
	if err := db.QueryRow("SELECT COALESCE(auto_checkout_midnight,0) FROM "+tbl("users")+" WHERE id=?", userID).Scan(&auto); err != nil {
		return
	}
	if auto == 0 {
		return
	}
	// get last entry and whether it was a working type
	var last time.Time
	var work int
	q := fmt.Sprintf("SELECT date, (SELECT work FROM %s t WHERE t.id = e.type_id) FROM %s e WHERE user_id=? ORDER BY date DESC LIMIT 1", tbl("type"), tbl("entries"))
	if err := db.QueryRow(q, userID).Scan(&last, &work); err != nil {
		return
	}
	if work != 1 {
		return
	}
	ly, lm, ld := last.Date()
	ny, nm, nd := now.Date()
	if ly == ny && lm == nm && ld == nd {
		return
	}
	midnight := time.Date(ly, lm, ld, 23, 59, 59, 0, last.Location())
	// find non-work activity (prefer Break)
	var nonWorkID int
	if err := db.QueryRow("SELECT id FROM " + tbl("type") + " WHERE work=0 ORDER BY CASE WHEN status='Break' THEN 0 ELSE 1 END, id LIMIT 1").Scan(&nonWorkID); err != nil {
		return
	}
	_, _ = db.Exec("INSERT INTO "+tbl("entries")+"(user_id, type_id, date) VALUES (?,?,?)", userID, nonWorkID, midnight)
}

// getUserEntriesDetailed returns detailed entries for a user within an optional date range [from, to]
func getUserEntriesDetailed(userID int, from, to string) []EntryDetail {
	db := getDB()
	defer db.Close()
	where := "WHERE e.user_id = @uid"
	if strings.TrimSpace(from) != "" {
		where += " AND date(e.date) >= date(@from)"
	}
	if strings.TrimSpace(to) != "" {
		where += " AND date(e.date) <= date(@to)"
	}
	query := fmt.Sprintf(`
        SELECT 
            e.id,
            u.id as user_id,
            u.name as user_name,
            COALESCE(d.name, 'No Department') as department,
            t.id as activity_id,
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
        %s
        ORDER BY e.date DESC
        LIMIT 2000
    `, tbl("entries"), tbl("entries"), tbl("entries"), tbl("users"), tbl("departments"), tbl("type"), where)
	args := []interface{}{sql.Named("uid", userID)}
	if strings.TrimSpace(from) != "" {
		args = append(args, sql.Named("from", from))
	}
	if strings.TrimSpace(to) != "" {
		args = append(args, sql.Named("to", to))
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("Query user entries failed: %v", err)
		return nil
	}
	defer rows.Close()
	var list []EntryDetail
	for rows.Next() {
		var e EntryDetail
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserName, &e.Department, &e.ActivityID, &e.Activity, &e.Date, &e.Start, &e.End, &e.Duration, &e.Comment); err != nil {
			log.Printf("Scan user entry failed: %v", err)
			continue
		}
		list = append(list, e)
	}
	return list
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
			log.Printf("updateUser with password failed: %v", err)
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
		log.Printf("updateUser failed: %v", err)
	}
}

// Lookup user by email
func getUserByEmail(email string) (User, bool) {
	db := getDB()
	defer db.Close()
	query := fmt.Sprintf("SELECT id, name, email, COALESCE(password,''), COALESCE(role,'user'), stampkey, position, COALESCE(department_id,0), COALESCE(auto_checkout_midnight,0) FROM %s WHERE email=@mail", tbl("users"))
	var u User
	if err := db.QueryRow(query, sql.Named("mail", email)).Scan(&u.ID, &u.Name, &u.Email, &u.Password, &u.Role, &u.Stampkey, &u.Position, &u.DepartmentID, &u.AutoCheckoutMidnight); err != nil {
		return User{}, false
	}
	return u, true
}

// Lookup user by name
func getUserByName(name string) (User, bool) {
	db := getDB()
	defer db.Close()
	query := fmt.Sprintf("SELECT id, name, stampkey, email, COALESCE(password,''), COALESCE(role,'user'), position, COALESCE(department_id,0), COALESCE(auto_checkout_midnight,0) FROM %s WHERE name=@name", tbl("users"))
	var u User
	if err := db.QueryRow(query, sql.Named("name", name)).Scan(&u.ID, &u.Name, &u.Stampkey, &u.Email, &u.Password, &u.Role, &u.Position, &u.DepartmentID, &u.AutoCheckoutMidnight); err != nil {
		return User{}, false
	}
	return u, true
}

// Return current status and timestamp for a user, if any
func getCurrentStatusForUserID(userID int) (status string, at time.Time, ok bool) {
	db := getDB()
	defer db.Close()
	row := db.QueryRow(fmt.Sprintf("SELECT status, date FROM %s WHERE user_id=@id", tbl("current_status")), sql.Named("id", userID))
	var s string
	var t time.Time
	if err := row.Scan(&s, &t); err != nil {
		return "", time.Time{}, false
	}
	return s, t, true
}

// Work hours filtered for a single user (by user name as in view)
func getWorkHoursDataForUser(userName string) []WorkHoursData {
	db := getDB()
	defer db.Close()
	rows, err := db.Query(fmt.Sprintf("SELECT user_name, work_date, work_hours FROM %s WHERE user_name=@u", tbl("work_hours")), sql.Named("u", userName))
	if err != nil {
		log.Printf("Query work_hours (user) failed: %v", err)
		return nil
	}
	defer rows.Close()
	var list []WorkHoursData
	for rows.Next() {
		var w WorkHoursData
		if err := rows.Scan(&w.UserName, &w.WorkDate, &w.WorkHours); err != nil {
			log.Printf("getWorkHoursDataForUser scan failed: %v", err)
			continue
		}
		list = append(list, w)
	}
	return list
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
		log.Printf("updateActivity failed: %v", err)
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
		log.Printf("updateEntry failed: %v", err)
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
		log.Printf("deleteEntry failed: %v", err)
	}
}

func getEntry(id string) EntryDetail {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
        SELECT 
            e.id,
            u.id as user_id,
            u.name as user_name,
            COALESCE(d.name, 'No Department') as department,
            t.id as activity_id,
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
		Scan(&e.ID, &e.UserID, &e.UserName, &e.Department, &e.ActivityID, &e.Activity, &e.Date, &e.Start, &e.End, &e.Duration, &e.Comment); err != nil {
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
		log.Printf("deleteActivity failed: %v", err)
	}
}

func deleteActivity(id string) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("DELETE FROM %s WHERE id=@id", tbl("type"))
	_, err := db.Exec(query, sql.Named("id", id))
	if err != nil {
		log.Printf("deleteDepartment failed: %v", err)
	}
}

func deleteDepartment(id string) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("DELETE FROM %s WHERE id=@id", tbl("departments"))
	_, err := db.Exec(query, sql.Named("id", id))
	if err != nil {
		log.Printf("deleteUser entries failed: %v", err)
	}
}

func deleteUser(id string) {
	db := getDB()
	defer db.Close()

	// First delete all entries for this user
	query := fmt.Sprintf("DELETE FROM %s WHERE user_id=@id", tbl("entries"))
	_, err := db.Exec(query, sql.Named("id", id))
	if err != nil {
		log.Printf("deleteUser failed: %v", err)
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
			log.Printf("getWorkHoursData scan failed: %v", err)
			continue
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
			log.Printf("getCurrentStatusData scan failed: %v", err)
			continue
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

// Daily per-user activity used for dashboard drill-down
type UserDailyActivity struct {
	UserName     string
	Department   string
	WorkHours    float64
	BreakHours   float64
	LastActivity string
	Status       string
}

type EntryDetail struct {
	ID         int
	UserID     int
	UserName   string
	Department string
	ActivityID int
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

// getUsersByDepartmentOnDay returns users in a department with their work/break hours on a specific day (YYYY-MM-DD)
func getUsersByDepartmentOnDay(deptName, day string) []UserDailyActivity {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
		SELECT 
			u.name AS user_name,
			COALESCE(d.name, 'No Department') AS department,
			COALESCE(SUM(CASE WHEN t.work = 1 THEN 
				(JULIANDAY(
					COALESCE(
						(SELECT MIN(next_e.date) FROM %s next_e 
						 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
						datetime('now')
					)
				) - JULIANDAY(e.date)) * 24
			ELSE 0 END), 0) AS work_hours,
			COALESCE(SUM(CASE WHEN t.work = 0 THEN 
				(JULIANDAY(
					COALESCE(
						(SELECT MIN(next_e.date) FROM %s next_e 
						 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
						datetime('now')
					)
				) - JULIANDAY(e.date)) * 24
			ELSE 0 END), 0) AS break_hours,
			MAX(e.date) AS last_activity,
			COALESCE((
				SELECT t2.status FROM %s e2
				JOIN %s t2 ON e2.type_id = t2.id
				WHERE e2.user_id = u.id AND DATE(e2.date) = ?
				ORDER BY e2.date DESC LIMIT 1
			), '') AS status
		FROM %s u
		LEFT JOIN %s d ON u.department_id = d.id
		LEFT JOIN %s e ON u.id = e.user_id AND DATE(e.date) = ?
		LEFT JOIN %s t ON e.type_id = t.id
		WHERE d.name = ?
		GROUP BY u.id, u.name, d.name
		ORDER BY work_hours DESC
	`, tbl("entries"), tbl("entries"), tbl("entries"), tbl("type"), tbl("users"), tbl("departments"), tbl("entries"), tbl("type"))

	rows, err := db.Query(query, day, day, deptName)
	if err != nil {
		log.Printf("Query users by department/day failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []UserDailyActivity
	for rows.Next() {
		var u UserDailyActivity
		if err := rows.Scan(&u.UserName, &u.Department, &u.WorkHours, &u.BreakHours, &u.LastActivity, &u.Status); err != nil {
			log.Printf("Scan user daily activity failed: %v", err)
			continue
		}
		list = append(list, u)
	}
	return list
}

// getUserActivitySummaryByDepartment filters overall user activity by department name
func getUserActivitySummaryByDepartment(deptName string) []UserActivitySummary {
	all := getUserActivitySummary()
	if deptName == "" {
		return all
	}
	out := make([]UserActivitySummary, 0, len(all))
	for _, u := range all {
		if u.Department == deptName {
			out = append(out, u)
		}
	}
	return out
}

// getDepartmentSummaryOnDay computes per-department hours for a specific day (YYYY-MM-DD)
func getDepartmentSummaryOnDay(day string) []DepartmentSummary {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
		SELECT 
			d.name AS department_name,
			COUNT(DISTINCT u.id) AS total_users,
			COALESCE(SUM(CASE WHEN t.work = 1 THEN 
				(JULIANDAY(
					COALESCE(
						(SELECT MIN(next_e.date) FROM %s next_e 
						 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
						datetime('now')
					)
				) - JULIANDAY(e.date)) * 24
			ELSE 0 END), 0) AS total_hours,
			CASE WHEN COUNT(DISTINCT u.id) > 0 
				THEN COALESCE(SUM(CASE WHEN t.work = 1 THEN 
					(JULIANDAY(
						COALESCE(
							(SELECT MIN(next_e.date) FROM %s next_e 
							 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
							datetime('now')
						)
					) - JULIANDAY(e.date)) * 24
				ELSE 0 END), 0) / COUNT(DISTINCT u.id)
				ELSE 0 END AS avg_hours_per_user
		FROM %s d
		LEFT JOIN %s u ON d.id = u.department_id
		LEFT JOIN %s e ON u.id = e.user_id AND DATE(e.date) = ?
		LEFT JOIN %s t ON e.type_id = t.id
		GROUP BY d.id, d.name
		ORDER BY total_hours DESC
	`, tbl("entries"), tbl("entries"), tbl("departments"), tbl("users"), tbl("entries"), tbl("type"))

	rows, err := db.Query(query, day)
	if err != nil {
		log.Printf("Query department summary on day failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []DepartmentSummary
	for rows.Next() {
		var d DepartmentSummary
		if err := rows.Scan(&d.DepartmentName, &d.TotalUsers, &d.TotalHours, &d.AvgHoursPerUser); err != nil {
			log.Printf("Scan department summary on day failed: %v", err)
			continue
		}
		list = append(list, d)
	}
	return list
}

func getEntriesWithDetails() []EntryDetail {
	db := getDB()
	defer db.Close()

	// Select next event end_time without doing duration math in SQL to avoid
	// timezone differences between SQLite datetime('now') (UTC) and local times.
	query := fmt.Sprintf(`
		SELECT 
			e.id,
			u.id as user_id,
			u.name as user_name,
			COALESCE(d.name, 'No Department') as department,
			t.id as activity_id,
			t.status as activity,
			e.date,
			e.date as start_time,
			(SELECT MIN(next_e.date) FROM %s next_e 
			 WHERE next_e.user_id = e.user_id AND next_e.date > e.date) as end_time,
			COALESCE(e.comment, '') as comment
		FROM %s e
		JOIN %s u ON e.user_id = u.id
		LEFT JOIN %s d ON u.department_id = d.id
		JOIN %s t ON e.type_id = t.id
		ORDER BY e.date DESC
		LIMIT 1000
	`, tbl("entries"), tbl("entries"), tbl("users"), tbl("departments"), tbl("type"))

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Query entries with details failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []EntryDetail
	for rows.Next() {
		var e EntryDetail
		var end sql.NullString
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserName, &e.Department, &e.ActivityID, &e.Activity, &e.Date, &e.Start, &end, &e.Comment); err != nil {
			log.Printf("Scan entry detail failed: %v", err)
			continue
		}
		// Compute duration in Go to respect local time and avoid SQLite now()/UTC quirks
		startTs := parseDBTimeInLoc(e.Start, time.Local)
		var endTs time.Time
		if end.Valid && strings.TrimSpace(end.String) != "" {
			endTs = parseDBTimeInLoc(end.String, time.Local)
			e.End = end.String
		} else {
			endTs = time.Now()
			e.End = ""
		}
		dur := endTs.Sub(startTs).Hours()
		if dur < 0 {
			dur = 0
		}
		e.Duration = dur
		list = append(list, e)
	}
	return list
}

// getEntriesForDepartmentOnDay returns entry details for a department on a specific day (YYYY-MM-DD)
func getEntriesForDepartmentOnDay(deptName, day string) []EntryDetail {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`
		SELECT 
			e.id,
			u.id as user_id,
			u.name as user_name,
			COALESCE(d.name, 'No Department') as department,
			t.id as activity_id,
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
		WHERE DATE(e.date) = ? AND d.name = ?
		ORDER BY u.name ASC, e.date ASC
	`, tbl("entries"), tbl("entries"), tbl("entries"), tbl("users"), tbl("departments"), tbl("type"))

	rows, err := db.Query(query, day, deptName)
	if err != nil {
		log.Printf("Query entries for dept/day failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []EntryDetail
	for rows.Next() {
		var e EntryDetail
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserName, &e.Department, &e.ActivityID, &e.Activity, &e.Date, &e.Start, &e.End, &e.Duration, &e.Comment); err != nil {
			log.Printf("Scan entry detail (dept/day) failed: %v", err)
			continue
		}
		list = append(list, e)
	}
	return list
}

// getCalendarEntries returns calendar entries for the specified date range with optional filters
func getCalendarEntries(startDate, endDate time.Time, userFilter, activityFilter string) []CalendarEntry {
	db := getDB()
	defer db.Close()

	// Build query with optional filters
	baseQuery := fmt.Sprintf(`
		SELECT 
			e.date,
			u.name as user_name,
			t.status as activity,
			t.work as is_work,
			COALESCE(
				(JULIANDAY(
					COALESCE(
						(SELECT MIN(next_e.date) FROM %s next_e 
						 WHERE next_e.user_id = e.user_id AND next_e.date > e.date), 
						datetime('now')
					)
				) - JULIANDAY(e.date)) * 24, 0
			) as hours
		FROM %s e
		INNER JOIN %s u ON u.id = e.user_id
		INNER JOIN %s t ON t.id = e.type_id
		WHERE e.date >= ? AND e.date <= ?`,
		tbl("entries"), tbl("entries"), tbl("users"), tbl("type"))

	var args []interface{}
	args = append(args, startDate.Format("2006-01-02 15:04:05"), endDate.Format("2006-01-02 23:59:59"))

	// Add user filter if specified
	if userFilter != "" {
		baseQuery += " AND u.id = ?"
		args = append(args, userFilter)
	}

	// Add activity filter if specified
	if activityFilter != "" {
		baseQuery += " AND t.id = ?"
		args = append(args, activityFilter)
	}

	baseQuery += " ORDER BY e.date"

	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		log.Printf("Query calendar entries failed: %v", err)
		return nil
	}
	defer rows.Close()

	var entries []CalendarEntry
	for rows.Next() {
		var entry CalendarEntry
		var isWork int
		if err := rows.Scan(&entry.Date, &entry.UserName, &entry.Activity, &isWork, &entry.Hours); err != nil {
			log.Printf("Scan calendar entry failed: %v", err)
			continue
		}
		entry.IsWork = isWork == 1
		entries = append(entries, entry)
	}

	return entries
}

// getEntriesWithDetailsFiltered returns filtered time entries with details
func getEntriesWithDetailsFiltered(fromDate, toDate, department, user, activity, limit string) []EntryDetail {
	db := getDB()
	defer db.Close()

	// Build dynamic query with filters
	query := fmt.Sprintf(`
        SELECT e.id, e.user_id, u.name as user_name, 
               COALESCE(d.name, 'No Department') as department,
               e.type_id, t.status as activity, 
               DATE(e.timestamp) as date,
               TIME(e.timestamp) as start_time,
               '' as end_time,
               0.0 as duration,
               COALESCE(e.comment, '') as comment
        FROM %s e
        LEFT JOIN %s u ON e.user_id = u.id
        LEFT JOIN %s d ON u.department_id = d.id  
        LEFT JOIN %s t ON e.type_id = t.id
        WHERE 1=1`, tbl("entries"), tbl("users"), tbl("departments"), tbl("type"))

	var args []interface{}

	// Add date range filters
	if fromDate != "" {
		query += " AND DATE(e.timestamp) >= ?"
		args = append(args, fromDate)
	}
	if toDate != "" {
		query += " AND DATE(e.timestamp) <= ?"
		args = append(args, toDate)
	}

	// Add department filter
	if department != "" && department != "0" {
		query += " AND u.department_id = ?"
		args = append(args, department)
	}

	// Add user filter
	if user != "" && user != "0" {
		query += " AND e.user_id = ?"
		args = append(args, user)
	}

	// Add activity filter
	if activity != "" && activity != "0" {
		query += " AND e.type_id = ?"
		args = append(args, activity)
	}

	query += " ORDER BY e.timestamp DESC"

	// Add limit for preview
	if limit != "" && limit != "0" {
		query += " LIMIT ?"
		if limitInt, err := strconv.Atoi(limit); err == nil {
			args = append(args, limitInt)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("Query filtered entries failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []EntryDetail
	for rows.Next() {
		var e EntryDetail
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserName, &e.Department, &e.ActivityID, &e.Activity, &e.Date, &e.Start, &e.End, &e.Duration, &e.Comment); err != nil {
			log.Printf("Scan filtered entry detail failed: %v", err)
			continue
		}
		list = append(list, e)
	}
	return list
}

// getWorkHoursDataFiltered returns filtered work hours data
func getWorkHoursDataFiltered(fromDate, toDate, user, limit string) []WorkHoursData {
	db := getDB()
	defer db.Close()

	// Build dynamic query with filters
	query := fmt.Sprintf(`
        SELECT user_name, work_date, work_hours 
        FROM %s
        WHERE 1=1`, tbl("work_hours"))

	var args []interface{}

	// Add date range filters
	if fromDate != "" {
		query += " AND work_date >= ?"
		args = append(args, fromDate)
	}
	if toDate != "" {
		query += " AND work_date <= ?"
		args = append(args, toDate)
	}

	// Add user filter
	if user != "" {
		query += " AND user_name = ?"
		args = append(args, user)
	}

	query += " ORDER BY work_date DESC"

	// Add limit for preview
	if limit != "" && limit != "0" {
		query += " LIMIT ?"
		if limitInt, err := strconv.Atoi(limit); err == nil {
			args = append(args, limitInt)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("Query filtered work hours failed: %v", err)
		return nil
	}
	defer rows.Close()

	var list []WorkHoursData
	for rows.Next() {
		var wh WorkHoursData
		if err := rows.Scan(&wh.UserName, &wh.WorkDate, &wh.WorkHours); err != nil {
			log.Printf("Scan filtered work hours failed: %v", err)
			continue
		}
		list = append(list, wh)
	}
	return list
}
