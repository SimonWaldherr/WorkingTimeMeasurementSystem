# Timekeeping System in Golang

[![DOI](https://zenodo.org/badge/630874153.svg)](https://zenodo.org/doi/10.5281/zenodo.13685441)

This project demonstrates how to create a simple timekeeping system using Golang and SQLite. The application allows users to clock in and out, track work hours, and view their current status. The data is organized by departments, users, and activities.

Please note that this project is meant for demonstration purposes only and does not include production-grade authentication, authorization, or security features. It showcases the ease of programming a timekeeping system in Golang.

## Getting Started

To run the project, follow these steps:

* Install Golang
* Clone the repository.
* Change to the project directory.
* Run go build to compile the project, or run directly with `go run .`.
* Alternatively, use `./test.sh` to start with a local SQLite DB (`time_tracking.test.db`).
* Multi-tenant: per-host data lives under `tenant/<host>/time_tracking.db` (auto-created).

## Usage

Before clocking in and out, you need to create a department, then a user and an activity. Follow these steps to start using the timekeeping system:

* Access the web interface at http://localhost:8083.
* Navigate to the /addDepartment page to create a new department.
* Navigate to the /addUser page to create a new user and associate them with a department.
* Navigate to the /addActivity page to create a new activity.
* Use the form on the index page to clock in and out by selecting a user and an activity from the dropdown menus.
* Users can view their own history at `/myHistory` (email + password).
* Admins can export CSVs from the Admin menu (Entries, Work Hours).
* Tenant overrides: place `templates/*.html` or `static/*` under `tenant/<host>/` to override defaults.

## Features

The timekeeping system includes the following features:

* Create and manage departments, users, and activities.
* Clock in and out with a user and an activity.
* View work hours per user per day.
* View the current status of all employees.
* Admin downloads: export Entries and Work Hours as CSV (`/admin/download/...`).
* User self‑service: personal history at `/myHistory` using email + password.
* Optional per‑user auto checkout at 23:59:59 (toggle in Edit User).

## Future Features

* check-in and -out with RFID tags ([like in this example](https://github.com/SimonWaldherr/rp2040-examples/blob/main/read_rfid_with_rc522.py)/[or here](https://github.com/SimonWaldherr/rp2040-examples/blob/main/read_rfid_with_rc522.go))
* Automatic generation of reports and analyses on work hours, productivity, and attendance
* Real-time notifications to managers when an employee works longer than planned
* automatically tracking and managing overtime, with options for compensatory days off or additional pay
* allow project-based time tracking, enabling employees to log hours to specific projects and tasks
* gamification elements to increase employee engagement, such as rewards for punctual clock-ins
* self-service portal where employees can manage their work hours, leave requests, and overtime applications themselves
* monitor compliance with labor laws and internal company policies
* verifies that all employees who were in the building during an emergency evacuation have reached the designated assembly point

## Limitations

This project is a simple demonstration and does not include the following:

* Authentication and authorization mechanisms.
* Security features, such as input validation and protection against SQL injections.
* Error handling and input validation for user interactions.
* A user-friendly and responsive user interface.

## License

This project is provided under the MIT License.
