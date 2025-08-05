-- Multi-Tenant Schema for MSSQL
-- Add tenant table and update existing tables with tenant_id

-- Create tenant table
IF OBJECT_ID('[dbo].[tenants]', 'U') IS NULL
CREATE TABLE [dbo].[tenants] (
    [id] INT IDENTITY(1,1) PRIMARY KEY,
    [name] NVARCHAR(255) NOT NULL,
    [subdomain] NVARCHAR(100) UNIQUE NOT NULL,
    [domain] NVARCHAR(255),
    [active] BIT DEFAULT 1,
    [config] NVARCHAR(MAX) DEFAULT '{}',
    [created_at] DATETIME2 DEFAULT GETDATE(),
    [updated_at] DATETIME2 DEFAULT GETDATE()
);
GO

-- Add tenant_id columns to existing tables if they don't exist
IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('[dbo].[departments]') AND name = 'tenant_id')
    ALTER TABLE [dbo].[departments] ADD [tenant_id] INT;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('[dbo].[users]') AND name = 'tenant_id')
    ALTER TABLE [dbo].[users] ADD [tenant_id] INT;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('[dbo].[type]') AND name = 'tenant_id')
    ALTER TABLE [dbo].[type] ADD [tenant_id] INT;
GO

IF NOT EXISTS (SELECT * FROM sys.columns WHERE object_id = OBJECT_ID('[dbo].[entries]') AND name = 'tenant_id')
    ALTER TABLE [dbo].[entries] ADD [tenant_id] INT;
GO

-- Add foreign key constraints
IF NOT EXISTS (SELECT * FROM sys.foreign_keys WHERE name = 'FK_departments_tenant')
    ALTER TABLE [dbo].[departments] ADD CONSTRAINT FK_departments_tenant FOREIGN KEY ([tenant_id]) REFERENCES [dbo].[tenants] ([id]);
GO

IF NOT EXISTS (SELECT * FROM sys.foreign_keys WHERE name = 'FK_users_tenant')
    ALTER TABLE [dbo].[users] ADD CONSTRAINT FK_users_tenant FOREIGN KEY ([tenant_id]) REFERENCES [dbo].[tenants] ([id]);
GO

IF NOT EXISTS (SELECT * FROM sys.foreign_keys WHERE name = 'FK_type_tenant')
    ALTER TABLE [dbo].[type] ADD CONSTRAINT FK_type_tenant FOREIGN KEY ([tenant_id]) REFERENCES [dbo].[tenants] ([id]);
GO

IF NOT EXISTS (SELECT * FROM sys.foreign_keys WHERE name = 'FK_entries_tenant')
    ALTER TABLE [dbo].[entries] ADD CONSTRAINT FK_entries_tenant FOREIGN KEY ([tenant_id]) REFERENCES [dbo].[tenants] ([id]);
GO

-- Create indexes for performance
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_departments_tenant_id')
    CREATE NONCLUSTERED INDEX IX_departments_tenant_id ON [dbo].[departments] ([tenant_id]);
GO

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_users_tenant_id')
    CREATE NONCLUSTERED INDEX IX_users_tenant_id ON [dbo].[users] ([tenant_id]);
GO

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_type_tenant_id')
    CREATE NONCLUSTERED INDEX IX_type_tenant_id ON [dbo].[type] ([tenant_id]);
GO

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_entries_tenant_id')
    CREATE NONCLUSTERED INDEX IX_entries_tenant_id ON [dbo].[entries] ([tenant_id]);
GO

-- Create default demo tenant
IF NOT EXISTS (SELECT * FROM [dbo].[tenants] WHERE subdomain = 'demo')
    INSERT INTO [dbo].[tenants] (name, subdomain, domain, active) 
    VALUES ('Demo Company', 'demo', 'demo.localhost', 1);
GO

-- Update existing data to belong to demo tenant
DECLARE @DemoTenantId INT = (SELECT id FROM [dbo].[tenants] WHERE subdomain = 'demo');

UPDATE [dbo].[departments] SET [tenant_id] = @DemoTenantId WHERE [tenant_id] IS NULL;
UPDATE [dbo].[users] SET [tenant_id] = @DemoTenantId WHERE [tenant_id] IS NULL;
UPDATE [dbo].[type] SET [tenant_id] = @DemoTenantId WHERE [tenant_id] IS NULL;
UPDATE [dbo].[entries] SET [tenant_id] = @DemoTenantId WHERE [tenant_id] IS NULL;
GO
