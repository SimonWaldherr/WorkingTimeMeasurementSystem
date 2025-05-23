-- Variablen für Datenbank und Schema
DECLARE @DatabaseName NVARCHAR(128) = N'WorkingTimeMeasurement';
DECLARE @SchemaName NVARCHAR(128) = N'dbo';

-- Datenbank und Schema erstellen (nur falls nötig)
IF DB_ID(@DatabaseName) IS NULL
    EXEC('CREATE DATABASE [' + @DatabaseName + ']');
GO

USE [WorkingTimeMeasurement];
GO

IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = @SchemaName)
    EXEC('CREATE SCHEMA [' + @SchemaName + ']');
GO

-- Tabelle: departments
IF OBJECT_ID(QUOTENAME(@SchemaName) + '.departments', 'U') IS NULL
CREATE TABLE [dbo].[departments] (
    [id] INT IDENTITY(1,1) PRIMARY KEY,
    [name] NVARCHAR(255) UNIQUE NOT NULL
);

-- Tabelle: users
IF OBJECT_ID(QUOTENAME(@SchemaName) + '.users', 'U') IS NULL
CREATE TABLE [dbo].[users] (
    [id] INT IDENTITY(1,1) PRIMARY KEY,
    [stampkey] NVARCHAR(255) NOT NULL,
    [name] NVARCHAR(255) NOT NULL,
    [email] NVARCHAR(255) UNIQUE NOT NULL,
    [position] NVARCHAR(255),
    [department_id] INT,
    FOREIGN KEY ([department_id]) REFERENCES [dbo].[departments] ([id])
);

-- Tabelle: type
IF OBJECT_ID(QUOTENAME(@SchemaName) + '.type', 'U') IS NULL
CREATE TABLE [dbo].[type] (
    [id] INT IDENTITY(1,1) PRIMARY KEY,
    [status] NVARCHAR(255) UNIQUE NOT NULL,
    [work] BIT NOT NULL,
    [comment] NVARCHAR(1024)
);

-- Tabelle: entries
IF OBJECT_ID(QUOTENAME(@SchemaName) + '.entries', 'U') IS NULL
CREATE TABLE [dbo].[entries] (
    [id] INT IDENTITY(1,1) PRIMARY KEY,
    [date] DATETIME NOT NULL,
    [type_id] INT NOT NULL,
    [user_id] INT NOT NULL,
    [comment] NVARCHAR(1024),
    FOREIGN KEY ([type_id]) REFERENCES [dbo].[type] ([id]),
    FOREIGN KEY ([user_id]) REFERENCES [dbo].[users] ([id])
);

-- View: work_hours
IF OBJECT_ID(QUOTENAME(@SchemaName) + '.work_hours', 'V') IS NOT NULL
    DROP VIEW [dbo].[work_hours];
GO

CREATE VIEW [dbo].[work_hours] AS
WITH work_intervals AS (
    SELECT
        u.name AS user_name,
        t.status,
        t.work,
        CAST(e.date AS DATETIME) AS start_time,
        ISNULL(
            (
                SELECT TOP 1 CAST(next_e.date AS DATETIME)
                FROM [dbo].[entries] AS next_e
                WHERE next_e.user_id = e.user_id
                  AND next_e.date > e.date
                ORDER BY next_e.date ASC
            ),
            GETDATE()
        ) AS end_time
    FROM
        [dbo].[entries] AS e
        INNER JOIN [dbo].[users] AS u ON u.id = e.user_id
        INNER JOIN [dbo].[type] AS t ON t.id = e.type_id
)
SELECT
    user_name,
    CAST(start_time AS DATE) AS work_date,
    ROUND(SUM(DATEDIFF(MINUTE, start_time, end_time) / 60.0 * work), 2) AS work_hours
FROM
    work_intervals
GROUP BY
    user_name,
    CAST(start_time AS DATE);
GO

-- View: current_status
IF OBJECT_ID(QUOTENAME(@SchemaName) + '.current_status', 'V') IS NOT NULL
    DROP VIEW [dbo].[current_status];
GO

CREATE VIEW [dbo].[current_status] AS
SELECT
    u.id AS user_id,
    u.name AS user_name,
    t.id AS type_id,
    t.status AS status,
    e.date AS date
FROM
    [dbo].[entries] e
    INNER JOIN (
        SELECT user_id, MAX(date) AS latest_date
        FROM [dbo].[entries]
        GROUP BY user_id
    ) latest_entry ON e.user_id = latest_entry.user_id AND e.date = latest_entry.latest_date
    INNER JOIN [dbo].[users] u ON u.id = e.user_id
    INNER JOIN [dbo].[type] t ON t.id = e.type_id;
GO

-- View: work_hours_with_type
IF OBJECT_ID(QUOTENAME(@SchemaName) + '.work_hours_with_type', 'V') IS NOT NULL
    DROP VIEW [dbo].[work_hours_with_type];
GO

CREATE VIEW [dbo].[work_hours_with_type] AS
WITH work_intervals AS (
    SELECT
        d.name AS department,
        u.name AS user_name,
        st.status AS type,
        st.work,
        CAST(we.date AS DATETIME) AS start_time,
        ISNULL(
            (
                SELECT TOP 1 CAST(next_we.date AS DATETIME)
                FROM [dbo].[entries] AS next_we
                WHERE next_we.user_id = we.user_id
                  AND next_we.date > we.date
                ORDER BY next_we.date ASC
            ),
            GETDATE()
        ) AS end_time
    FROM
        [dbo].[entries] AS we
        INNER JOIN [dbo].[users] AS u ON u.id = we.user_id
        INNER JOIN [dbo].[departments] AS d ON d.id = u.department_id
        INNER JOIN [dbo].[type] AS st ON st.id = we.type_id
)
SELECT
    department,
    user_name,
    type,
    start_time,
    ROUND(SUM(DATEDIFF(MINUTE, start_time, end_time) / 60.0), 2) AS work_hours
FROM
    work_intervals
WHERE
    work = 1
GROUP BY
    department,
    user_name,
    type,
    start_time;
GO

-- View: entries_view
IF OBJECT_ID(QUOTENAME(@SchemaName) + '.entries_view', 'V') IS NOT NULL
    DROP VIEW [dbo].[entries_view];
GO

CREATE VIEW [dbo].[entries_view] AS
SELECT 
    e.id,
    e.date AS [Date],
    t.status AS StatusType,
    t.work AS isWork,
    t.comment AS typeComment,
    u.name AS UserName,
    u.email AS eMail,
    d.name AS Department
FROM [dbo].[entries] e
    LEFT JOIN [dbo].[type] t ON e.type_id = t.id
    LEFT JOIN [dbo].[users] u ON e.user_id = u.id
    LEFT JOIN [dbo].[departments] d ON u.department_id = d.id;
GO