-- Insert departments
INSERT INTO departments (name) VALUES ('Sales');
INSERT INTO departments (name) VALUES ('Marketing');
INSERT INTO departments (name) VALUES ('Engineering');

-- Insert users
INSERT INTO users (name, department_id, email) VALUES ('John Doe', 1, 'jd@example.tld');
INSERT INTO users (name, department_id, email) VALUES ('Jane Smith', 2, 'js@example.tld');
INSERT INTO users (name, department_id, email) VALUES ('Alice Johnson', 3, 'aj@example.tld');

-- Insert type/status
INSERT INTO type (status, work, comment) VALUES ('work', 1, 'clock in');
INSERT INTO type (status, work, comment) VALUES ('end of work', 0, 'clock out');
INSERT INTO type (status, work, comment) VALUES ('break', 0, 'clock out');
INSERT INTO type (status, work, comment) VALUES ('clean up', 1, 'additional activities');

-- Insert entries
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-11 09:00:00', 1, 1, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-11 17:00:00', 2, 1, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-12 09:00:00', 1, 1, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-12 12:00:00', 3, 1, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-12 13:00:00', 1, 1, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-12 17:00:00', 2, 1, '');

INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-11 10:00:00', 1, 2, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-11 18:00:00', 2, 2, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-12 10:00:00', 1, 2, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-12 18:00:00', 2, 2, '');

INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-11 09:30:00', 1, 3, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-11 17:30:00', 2, 3, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-12 09:30:00', 1, 3, '');
INSERT INTO entries (date, type_id, user_id, comment) VALUES ('2022-04-12 17:30:00', 2, 3, '');
