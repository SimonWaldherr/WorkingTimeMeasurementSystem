# Timekeeping System in Golang

This project demonstrates how to create a simple timekeeping system using Golang and SQLite. The application allows users to clock in and out, track work hours, and view their current status. The data is organized by departments, users, and activities.

Please note that this project is meant for demonstration purposes only and does not include any authentication, authorization, or security features. It is meant to showcase the ease of programming a timekeeping system in Golang.

## Getting Started

To run the project, follow these steps:

* Install Golang
* Clone the repository.
* Change to the project directory.
* Run go build to compile the project.
* Run the compiled binary to start the web server.

## Usage

Before clocking in and out, you need to create a department, then a user and an activity. Follow these steps to start using the timekeeping system:

* Access the web interface at http://localhost:8080.
* Navigate to the /addDepartment page to create a new department.
* Navigate to the /addUser page to create a new user and associate them with a department.
* Navigate to the /addActivity page to create a new activity.
* Use the form on the index page to clock in and out by selecting a user and an activity from the dropdown menus.

## Features

The timekeeping system includes the following features:

* Create and manage departments, users, and activities.
* Clock in and out with a user and an activity.
* View work hours per user per day.
* View the current status of all employees.

## Limitations

This project is a simple demonstration and does not include the following:

* Authentication and authorization mechanisms.
* Security features, such as input validation and protection against SQL injections.
* Error handling and input validation for user interactions.
* A user-friendly and responsive user interface.
