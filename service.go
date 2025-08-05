package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"
)

// Service layer for business logic
type WorkingTimeService struct {
	db *sql.DB
}

// NewWorkingTimeService creates a new service instance
func NewWorkingTimeService() *WorkingTimeService {
	return &WorkingTimeService{
		db: getDB(),
	}
}

// Close closes the database connection
func (s *WorkingTimeService) Close() error {
	return s.db.Close()
}

// getUsersForTenant retrieves users for a specific tenant
func (s *WorkingTimeService) GetUsersForTenant(ctx context.Context, tenantID int) ([]User, error) {
	query := fmt.Sprintf(`SELECT id, name, email, position, department_id, stampkey, tenant_id 
						 FROM %s WHERE tenant_id = ?`, tbl("users"))
	
	rows, err := s.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Position, &u.DepartmentID, &u.Stampkey, &u.TenantID); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// getActivitiesForTenant retrieves activities for a specific tenant
func (s *WorkingTimeService) GetActivitiesForTenant(ctx context.Context, tenantID int) ([]Activity, error) {
	query := fmt.Sprintf(`SELECT id, status, work, comment, tenant_id 
						 FROM %s WHERE tenant_id = ?`, tbl("type"))
	
	rows, err := s.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []Activity
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.Status, &a.Work, &a.Comment, &a.TenantID); err != nil {
			return nil, err
		}
		activities = append(activities, a)
	}
	return activities, nil
}

// getDepartmentsForTenant retrieves departments for a specific tenant
func (s *WorkingTimeService) GetDepartmentsForTenant(ctx context.Context, tenantID int) ([]Department, error) {
	query := fmt.Sprintf(`SELECT id, name, tenant_id 
						 FROM %s WHERE tenant_id = ?`, tbl("departments"))
	
	rows, err := s.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var departments []Department
	for rows.Next() {
		var d Department
		if err := rows.Scan(&d.ID, &d.Name, &d.TenantID); err != nil {
			return nil, err
		}
		departments = append(departments, d)
	}
	return departments, nil
}

// createUserForTenant creates a new user for a specific tenant
func (s *WorkingTimeService) CreateUserForTenant(ctx context.Context, tenantID int, name, stampkey, email, position string, departmentID int) error {
	if stampkey == "" {
		// Generate unique stampkey
		stampkey = strconv.Itoa(s.createUniqueStampKey(ctx, tenantID))
	} else {
		// Check if stampkey already exists for this tenant
		if exists, err := s.stampKeyExistsForTenant(ctx, tenantID, stampkey); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("stampkey %s already exists for this tenant", stampkey)
		}
	}

	query := fmt.Sprintf(`INSERT INTO %s (name, stampkey, email, position, department_id, tenant_id)
						 VALUES (?, ?, ?, ?, ?, ?)`, tbl("users"))
	
	_, err := s.db.ExecContext(ctx, query, name, stampkey, email, position, departmentID, tenantID)
	return err
}

// createActivityForTenant creates a new activity for a specific tenant
func (s *WorkingTimeService) CreateActivityForTenant(ctx context.Context, tenantID int, status, comment string, work int) error {
	query := fmt.Sprintf(`INSERT INTO %s (status, work, comment, tenant_id)
						 VALUES (?, ?, ?, ?)`, tbl("type"))
	
	_, err := s.db.ExecContext(ctx, query, status, work, comment, tenantID)
	return err
}

// createDepartmentForTenant creates a new department for a specific tenant
func (s *WorkingTimeService) CreateDepartmentForTenant(ctx context.Context, tenantID int, name string) error {
	query := fmt.Sprintf(`INSERT INTO %s (name, tenant_id) VALUES (?, ?)`, tbl("departments"))
	
	_, err := s.db.ExecContext(ctx, query, name, tenantID)
	return err
}

// createEntryForTenant creates a new time entry for a specific tenant
func (s *WorkingTimeService) CreateEntryForTenant(ctx context.Context, tenantID int, userID, activityID string, entryDate time.Time) error {
	query := fmt.Sprintf(`INSERT INTO %s (user_id, type_id, date, tenant_id)
						 VALUES (?, ?, ?, ?)`, tbl("entries"))
	
	_, err := s.db.ExecContext(ctx, query, userID, activityID, entryDate, tenantID)
	return err
}

// Helper methods
func (s *WorkingTimeService) createUniqueStampKey(ctx context.Context, tenantID int) int {
	for {
		stampKey := time.Now().UnixNano()%900000000000 + 100000000000 // 12-digit
		
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE stampkey = ? AND tenant_id = ?`, tbl("users"))
		var count int
		if err := s.db.QueryRowContext(ctx, query, stampKey, tenantID).Scan(&count); err != nil {
			log.Printf("Error checking stampkey uniqueness: %v", err)
			continue
		}
		
		if count == 0 {
			return int(stampKey)
		}
	}
}

func (s *WorkingTimeService) stampKeyExistsForTenant(ctx context.Context, tenantID int, stampkey string) (bool, error) {
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE stampkey = ? AND tenant_id = ?`, tbl("users"))
	var count int
	err := s.db.QueryRowContext(ctx, query, stampkey, tenantID).Scan(&count)
	return count > 0, err
}

// getUserIDFromStampKeyForTenant gets user ID from stamp key within tenant context
func (s *WorkingTimeService) GetUserIDFromStampKeyForTenant(ctx context.Context, tenantID int, stampKey string) (string, error) {
	query := fmt.Sprintf(`SELECT id FROM %s WHERE stampkey = ? AND tenant_id = ?`, tbl("users"))
	var id string
	err := s.db.QueryRowContext(ctx, query, stampKey, tenantID).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // Not found, but not an error
		}
		return "", err
	}
	return id, nil
}

// getWorkHoursDataForTenant retrieves work hours data for a specific tenant
func (s *WorkingTimeService) GetWorkHoursDataForTenant(ctx context.Context, tenantID int) ([]WorkHoursData, error) {
	// Note: This would require updating the view to include tenant filtering
	query := fmt.Sprintf(`
		SELECT user_name, work_date, work_hours 
		FROM %s 
		WHERE tenant_id = ?`, tbl("work_hours"))
	
	rows, err := s.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		log.Printf("Query work_hours failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var list []WorkHoursData
	for rows.Next() {
		var w WorkHoursData
		if err := rows.Scan(&w.UserName, &w.WorkDate, &w.WorkHours); err != nil {
			return nil, err
		}
		list = append(list, w)
	}
	return list, nil
}

// getCurrentStatusDataForTenant retrieves current status data for a specific tenant
func (s *WorkingTimeService) GetCurrentStatusDataForTenant(ctx context.Context, tenantID int) ([]CurrentStatusData, error) {
	// Note: This would require updating the view to include tenant filtering
	query := fmt.Sprintf(`
		SELECT user_name, status, date 
		FROM %s 
		WHERE tenant_id = ?`, tbl("current_status"))
	
	rows, err := s.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		log.Printf("Query current_status failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var list []CurrentStatusData
	for rows.Next() {
		var c CurrentStatusData
		if err := rows.Scan(&c.UserName, &c.Status, &c.Date); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, nil
}
