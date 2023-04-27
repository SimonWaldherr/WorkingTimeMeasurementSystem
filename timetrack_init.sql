CREATE TABLE
IF NOT EXISTS "type"(
	"id" INTEGER PRIMARY KEY,
	"status" TEXT UNIQUE NOT NULL,
	"work" INTEGER NOT NULL,
	"comment" TEXT
);


CREATE TABLE
IF NOT EXISTS "entries"(
	"id" INTEGER PRIMARY KEY,
	"date" DATETIMENOT NULL,
	"type_id" INTEGER NOT NULL,
	"user_id" INTEGER NOT NULL,
	"comment" TEXT,
	FOREIGN KEY("type_id") REFERENCES "type"("id"),
	FOREIGN KEY("user_id") REFERENCES "users"("id")
);


CREATE TABLE
IF NOT EXISTS "users"(
	"id" INTEGER PRIMARY KEY,
	"stampkey" TEXT NOT NULL,
	"name" TEXT NOT NULL,
	"email" TEXT UNIQUE NOT NULL,
	"position" TEXT,
	"department_id" INTEGER,
	FOREIGN KEY("department_id") REFERENCES "departments"("id")
);


CREATE TABLE
IF NOT EXISTS "departments"(
	"id" INTEGER PRIMARY KEY,
	"name" TEXT UNIQUE NOT NULL
);


CREATE VIEW
IF NOT EXISTS "work_hours" AS WITH work_intervals AS(
	SELECT
		u.name AS user_name,
		t.status,
		t.work,
		DATETIME(e.date) AS start_time,
		IFNULL(
			(
				SELECT
					DATETIME(next_e.date)
				FROM
					entries AS next_e
				WHERE
					next_e.user_id = e.user_id
				AND DATETIME(next_e.date) > DATETIME(e.date)
				ORDER BY
					DATETIME(next_e.date) ASC
				LIMIT 0,
				1
			),
			DATETIME('now')
		) AS end_time
	FROM
		entries AS e
	JOIN users AS u ON u.id = e.user_id
	JOIN type AS t ON t.id = e.type_id
	ORDER BY
		DATETIME(e.date) ASC
) SELECT
	user_name,
	DATE(start_time) AS work_date,
	ROUND(
		SUM(
			(
				JULIANDAY(end_time) - JULIANDAY(start_time)
			) * 24 * work
		),
		2
	) AS work_hours
FROM
	work_intervals
GROUP BY
	user_name,
	DATE(start_time);


CREATE VIEW
IF NOT EXISTS "current_status" AS SELECT
	users.id AS user_id,
	users.name AS user_name,
	type.id AS type_id,
	type.status AS status,
	entries.date AS date
FROM
	entries
JOIN(
	SELECT
		user_id,
		MAX(date) AS latest_date
	FROM
		entries
	GROUP BY
		user_id
) AS latest_entry ON entries.user_id = latest_entry.user_id
AND entries.date = latest_entry.latest_date
JOIN users ON users.id = entries.user_id
JOIN type ON type.id = entries.type_id;


CREATE VIEW
IF NOT EXISTS "work_hours_with_type" AS WITH work_intervals AS (
  SELECT
    d.name AS department,
    u.name AS user_name,
    st.status as type,
    st.work,
    DATETIME(we.date) AS start_time,
    IFNULL(
      (
        SELECT
          DATETIME(next_we.date)
        FROM
          entries AS next_we
        WHERE
          next_we.user_id = we.user_id
          AND DATETIME(next_we.date) > DATETIME(we.date)
        ORDER BY
          DATETIME(next_we.date) ASC
        LIMIT 0, 1
      ),
      DATETIME('now')
    ) AS end_time
  FROM
    entries AS we
  JOIN
    users AS u ON u.id = we.user_id
  JOIN
    departments AS d ON d.id = u.department_id
  JOIN
    type AS st ON st.id = we.type_id
  ORDER BY
    DATETIME(we.date) ASC
)

SELECT
  department,
  user_name,
  type,
  start_time,
  ROUND(SUM((JULIANDAY(end_time) - JULIANDAY(start_time)) * 24), 2) AS work_hours
FROM
  work_intervals
WHERE
  work = 1
GROUP BY
  department,
  user_name,
  type,
  start_time;


CREATE VIEW
IF NOT EXISTS "entries_view" AS
SELECT 
	entries.id, 
	entries.date as Date, 
	type.status as StatusType, 
	type.work as isWork, 
	type.comment as typeComment, 
	users.name as UserName, 
	users.email as eMail, 
	departments.name as Department 
FROM entries
LEFT JOIN type ON entries.type_id = type.id
LEFT JOIN users ON entries.user_id = users.id
LEFT JOIN departments ON users.department_id = departments.id;
