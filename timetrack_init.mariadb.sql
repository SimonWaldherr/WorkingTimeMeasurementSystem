-- MariaDB/MySQL schema for Working Time Measurement System
CREATE TABLE IF NOT EXISTS `departments` (
  `id` INT AUTO_INCREMENT PRIMARY KEY,
  `name` VARCHAR(255) UNIQUE NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `users` (
  `id` INT AUTO_INCREMENT PRIMARY KEY,
  `stampkey` VARCHAR(255) NOT NULL,
  `name` VARCHAR(255) NOT NULL,
  `email` VARCHAR(255) UNIQUE NOT NULL,
  `position` VARCHAR(255),
  `department_id` INT,
  CONSTRAINT `fk_users_department` FOREIGN KEY (`department_id`) REFERENCES `departments`(`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `type` (
  `id` INT AUTO_INCREMENT PRIMARY KEY,
  `status` VARCHAR(255) UNIQUE NOT NULL,
  `work` TINYINT(1) NOT NULL,
  `comment` VARCHAR(1024)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `entries` (
  `id` INT AUTO_INCREMENT PRIMARY KEY,
  `date` DATETIME NOT NULL,
  `type_id` INT NOT NULL,
  `user_id` INT NOT NULL,
  `comment` VARCHAR(1024),
  CONSTRAINT `fk_entries_type` FOREIGN KEY (`type_id`) REFERENCES `type`(`id`),
  CONSTRAINT `fk_entries_user` FOREIGN KEY (`user_id`) REFERENCES `users`(`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Views
DROP VIEW IF EXISTS `work_hours`;
CREATE VIEW `work_hours` AS
WITH work_intervals AS (
  SELECT
    u.name AS user_name,
    t.status,
    t.work,
    CAST(e.date AS DATETIME) AS start_time,
    COALESCE(
      (
        SELECT CAST(next_e.date AS DATETIME)
        FROM `entries` AS next_e
        WHERE next_e.user_id = e.user_id
          AND next_e.date > e.date
        ORDER BY next_e.date ASC
        LIMIT 1
      ),
      NOW()
    ) AS end_time
  FROM `entries` e
  JOIN `users` u ON u.id = e.user_id
  JOIN `type` t ON t.id = e.type_id
)
SELECT
  user_name,
  DATE(start_time) AS work_date,
  ROUND(SUM(TIMESTAMPDIFF(MINUTE, start_time, end_time) / 60.0 * work), 2) AS work_hours
FROM work_intervals
GROUP BY user_name, DATE(start_time);

DROP VIEW IF EXISTS `current_status`;
CREATE VIEW `current_status` AS
SELECT
  u.id AS user_id,
  u.name AS user_name,
  t.id AS type_id,
  t.status AS status,
  e.date AS date
FROM `entries` e
JOIN (
  SELECT user_id, MAX(date) AS latest_date
  FROM `entries`
  GROUP BY user_id
) latest_entry ON e.user_id = latest_entry.user_id AND e.date = latest_entry.latest_date
JOIN `users` u ON u.id = e.user_id
JOIN `type` t ON t.id = e.type_id;

DROP VIEW IF EXISTS `work_hours_with_type`;
CREATE VIEW `work_hours_with_type` AS
WITH work_intervals AS (
  SELECT
    d.name AS department,
    u.name AS user_name,
    st.status AS type,
    st.work,
    CAST(we.date AS DATETIME) AS start_time,
    COALESCE(
      (
        SELECT CAST(next_we.date AS DATETIME)
        FROM `entries` AS next_we
        WHERE next_we.user_id = we.user_id
          AND next_we.date > we.date
        ORDER BY next_we.date ASC
        LIMIT 1
      ),
      NOW()
    ) AS end_time
  FROM `entries` we
  JOIN `users` u ON u.id = we.user_id
  JOIN `departments` d ON d.id = u.department_id
  JOIN `type` st ON st.id = we.type_id
)
SELECT
  department,
  user_name,
  type,
  start_time,
  ROUND(SUM(TIMESTAMPDIFF(MINUTE, start_time, end_time) / 60.0), 2) AS work_hours
FROM work_intervals
WHERE work = 1
GROUP BY department, user_name, type, start_time;
