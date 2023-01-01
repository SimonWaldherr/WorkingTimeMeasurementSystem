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

