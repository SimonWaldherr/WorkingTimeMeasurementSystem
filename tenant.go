package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// Tenant represents a client/organization in the multi-tenant system
type Tenant struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Subdomain string `json:"subdomain"`
	Domain   string `json:"domain"`
	Active   bool   `json:"active"`
	Config   string `json:"config"` // JSON config for tenant-specific settings
}

// TenantContext holds tenant information for request context
type TenantContext struct {
	Tenant   *Tenant
	TenantID int
}

var (
	ErrTenantNotFound = errors.New("tenant not found")
	ErrInvalidTenant  = errors.New("invalid tenant")
)

// getTenantFromHost extracts tenant information from the request host
func getTenantFromHost(host string) (*Tenant, error) {
	// Remove port if present
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return nil, ErrTenantNotFound
	}

	subdomain := parts[0]
	
	// Skip 'www' subdomain
	if subdomain == "www" && len(parts) > 2 {
		subdomain = parts[1]
	}

	// For localhost development, use the full host as subdomain
	if strings.Contains(host, "localhost") || strings.Contains(host, "127.0.0.1") {
		subdomain = "demo" // Default tenant for local development
	}

	return getTenantBySubdomain(subdomain)
}

// getTenantBySubdomain retrieves tenant by subdomain
func getTenantBySubdomain(subdomain string) (*Tenant, error) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("SELECT id, name, subdomain, domain, active, config FROM %s WHERE subdomain = ? AND active = 1", tbl("tenants"))
	
	var tenant Tenant
	err := db.QueryRow(query, subdomain).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Subdomain,
		&tenant.Domain,
		&tenant.Active,
		&tenant.Config,
	)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrTenantNotFound
		}
		return nil, err
	}

	return &tenant, nil
}

// tenantMiddleware adds tenant context to requests
func tenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant, err := getTenantFromHost(r.Host)
		if err != nil {
			log.Printf("Tenant resolution failed for host %s: %v", r.Host, err)
			http.Error(w, "Invalid tenant", http.StatusBadRequest)
			return
		}

		if !tenant.Active {
			http.Error(w, "Tenant suspended", http.StatusServiceUnavailable)
			return
		}

		// Add tenant to request context
		ctx := context.WithValue(r.Context(), "tenant", &TenantContext{
			Tenant:   tenant,
			TenantID: tenant.ID,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getTenantFromContext extracts tenant from request context
func getTenantFromContext(ctx context.Context) (*TenantContext, error) {
	tenantCtx, ok := ctx.Value("tenant").(*TenantContext)
	if !ok {
		return nil, ErrInvalidTenant
	}
	return tenantCtx, nil
}

// createTenant creates a new tenant
func createTenant(name, subdomain, domain string) error {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf(`INSERT INTO %s (name, subdomain, domain, active, config) 
						 VALUES (?, ?, ?, 1, '{}')`, tbl("tenants"))
	_, err := db.Exec(query, name, subdomain, domain)
	return err
}

// getAllTenants retrieves all tenants
func getAllTenants() ([]Tenant, error) {
	db := getDB()
	defer db.Close()

	query := fmt.Sprintf("SELECT id, name, subdomain, domain, active, config FROM %s ORDER BY name", tbl("tenants"))
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []Tenant
	for rows.Next() {
		var tenant Tenant
		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.Subdomain,
			&tenant.Domain,
			&tenant.Active,
			&tenant.Config,
		)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, tenant)
	}

	return tenants, nil
}
