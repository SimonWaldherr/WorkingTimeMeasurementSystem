-- Multi-Tenant Schema for SQLite
-- Add tenant table and update existing tables with tenant_id

-- Tenant table
CREATE TABLE IF NOT EXISTS "tenants"(
	"id" INTEGER PRIMARY KEY,
	"name" TEXT NOT NULL,
	"subdomain" TEXT UNIQUE NOT NULL,
	"domain" TEXT,
	"active" INTEGER DEFAULT 1,
	"config" TEXT DEFAULT '{}',
	"created_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
	"updated_at" DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Update existing tables to include tenant_id
ALTER TABLE "departments" ADD COLUMN "tenant_id" INTEGER REFERENCES "tenants"("id");
ALTER TABLE "users" ADD COLUMN "tenant_id" INTEGER REFERENCES "tenants"("id");
ALTER TABLE "type" ADD COLUMN "tenant_id" INTEGER REFERENCES "tenants"("id");
ALTER TABLE "entries" ADD COLUMN "tenant_id" INTEGER REFERENCES "tenants"("id");

-- Add indexes for better performance
CREATE INDEX IF NOT EXISTS "idx_departments_tenant" ON "departments"("tenant_id");
CREATE INDEX IF NOT EXISTS "idx_users_tenant" ON "users"("tenant_id");
CREATE INDEX IF NOT EXISTS "idx_type_tenant" ON "type"("tenant_id");
CREATE INDEX IF NOT EXISTS "idx_entries_tenant" ON "entries"("tenant_id");

-- Create default demo tenant
INSERT OR IGNORE INTO "tenants" (id, name, subdomain, domain, active) 
VALUES (1, 'Demo Company', 'demo', 'demo.localhost', 1);

-- Update existing data to belong to demo tenant (only if there's no tenant_id set)
UPDATE "departments" SET "tenant_id" = 1 WHERE "tenant_id" IS NULL;
UPDATE "users" SET "tenant_id" = 1 WHERE "tenant_id" IS NULL;
UPDATE "type" SET "tenant_id" = 1 WHERE "tenant_id" IS NULL;
UPDATE "entries" SET "tenant_id" = 1 WHERE "tenant_id" IS NULL;
