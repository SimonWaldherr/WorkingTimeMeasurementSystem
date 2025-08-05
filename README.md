# Working Time Measurement System

[![DOI](https://zenodo.org/badge/630874153.svg)](https://zenodo.org/doi/10.5281/zenodo.13685441)

A comprehensive time tracking system built with Go, supporting both single-tenant and multi-tenant deployments. The application allows organizations to track employee work hours, manage departments, users, and activities with advanced features like barcode scanning and detailed reporting.

## Features

### Core Features
- **Multi-Tenant Support**: Single installation for multiple organizations/clients
- **Department Management**: Organize users by departments
- **User Management**: Create, edit, and manage employee profiles
- **Activity Tracking**: Define different work activities and track time
- **Clock In/Out System**: Manual and barcode-based time tracking
- **Real-time Status**: View current employee status (in/out)
- **Work Hours Reporting**: Detailed reports on work hours per user/day

### Advanced Features
- **Barcode Scanning**: RFID/Barcode integration for quick clock in/out
- **Multi-Database Support**: SQLite for development, MSSQL for production
- **Configurable Security**: Session management, rate limiting, CSRF protection
- **Docker Support**: Easy deployment with Docker and Docker Compose
- **Nginx Integration**: Reverse proxy for multi-tenant routing
- **Template System**: Embedded templates with external override support

## Quick Start

### Prerequisites
- Go 1.21 or later
- SQLite (for development) or MSSQL Server (for production)
- Docker & Docker Compose (for containerized deployment)

### Local Development

1. **Clone the repository**
   ```bash
   git clone https://github.com/SimonWaldherr/WorkingTimeMeasurementSystem.git
   cd WorkingTimeMeasurementSystem
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Configure the application**
   ```bash
   cp config.example.json config.json
   # Edit config.json according to your needs
   ```

4. **Run the application**
   ```bash
   go run .
   ```

5. **Access the application**
   - Single-tenant: http://localhost:8083
   - Multi-tenant: http://demo.localhost:8083 (requires /etc/hosts entry)

### Docker Deployment

1. **Basic deployment with SQLite**
   ```bash
   docker-compose up -d
   ```

2. **Production deployment with MSSQL**
   ```bash
   docker-compose --profile with-mssql up -d
   ```

3. **Full deployment with Nginx proxy**
   ```bash
   docker-compose --profile with-proxy --profile with-mssql up -d
   ```

## Configuration

The application supports both environment variables and JSON configuration files. Configuration priority:
1. Environment variables (highest priority)
2. config.json file
3. Default values (lowest priority)

### Key Configuration Options

```json
{
  "database": {
    "backend": "sqlite",           // "sqlite" or "mssql"
    "auto_migrate": true
  },
  "server": {
    "port": 8083,
    "host": ""
  },
  "security": {
    "session_secret": "your-secret-key",
    "session_duration": 30,        // minutes
    "csrf_protection": false,
    "rate_limiting": false
  },
  "features": {
    "multi_tenant": true,
    "barcode_scanning": true,
    "reporting": true,
    "email_notifications": false
  }
}
```

### Environment Variables

- `DB_BACKEND`: Database type (sqlite/mssql)
- `SQLITE_PATH`: SQLite database file path
- `MSSQL_SERVER`: MSSQL server address
- `FEATURE_MULTI_TENANT`: Enable multi-tenant mode
- `SESSION_SECRET`: Secret key for session encryption
- `SERVER_PORT`: HTTP server port

## Multi-Tenant Setup

### 1. Enable Multi-Tenant Mode
Set `FEATURE_MULTI_TENANT=true` or configure in config.json

### 2. Configure Tenant Routing
- **Subdomain-based**: `tenant1.yourdomain.com`, `tenant2.yourdomain.com`
- **Development**: Use /etc/hosts entries for local testing

### 3. Create Tenants
Tenants are created automatically when first accessed or manually via database:

```sql
INSERT INTO tenants (name, subdomain, domain, active) 
VALUES ('Company A', 'companya', 'companya.yourdomain.com', 1);
```

### 4. Nginx Configuration
Use the provided nginx.conf for proper multi-tenant routing with SSL/TLS support.

## Usage Guide

### Initial Setup

1. **Create a Department**
   - Navigate to `/addDepartment`
   - Enter department name (e.g., "Sales", "Engineering")

2. **Add Users**
   - Navigate to `/addUser`
   - Fill in user details and assign to department
   - Stamp keys are auto-generated if not provided

3. **Define Activities**
   - Navigate to `/addActivity`
   - Create activities like "Work", "Break", "Meeting"
   - Mark whether activity counts as work time

4. **Start Tracking**
   - Use `/clockInOutForm` for manual time tracking
   - Use `/scan` for barcode-based bulk operations

### Barcode Integration

The system supports barcode/RFID cards for users and activity codes:
- User codes: `USR-{stampkey}-END`
- Activity codes: `ACT-{activity_id}-END`

Generate printable barcode labels at `/barcodes`

## API Documentation

### Clock In/Out Endpoint
```http
POST /clockInOut
Content-Type: application/x-www-form-urlencoded

stampkey=123456&activity_id=1
```

### Bulk Clock Endpoint
```http
POST /bulkClock
Content-Type: application/json

{
  "activityCode": "ACT-1-END",
  "userCodes": ["USR-123456-END", "USR-789012-END"]
}
```

## Database Schema

### Core Tables
- `tenants`: Multi-tenant configuration
- `departments`: Organizational units
- `users`: Employee information
- `type`: Activity definitions
- `entries`: Time tracking records

### Views
- `work_hours`: Aggregated work hours per user/day
- `current_status`: Latest status for each user
- `work_hours_with_type`: Detailed time breakdown by activity

## Security Considerations

### Production Deployment
- Use strong session secrets (32+ characters)
- Enable HTTPS with proper SSL certificates
- Configure rate limiting and CSRF protection
- Use MSSQL with proper authentication
- Regular security updates and monitoring

### Authentication
- CSV-based authentication (demo only)
- Session-based security
- Tenant isolation in multi-tenant mode

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Future Roadmap

- [ ] Advanced reporting with charts and analytics
- [ ] RFID integration examples for Raspberry Pi
- [ ] REST API for mobile applications
- [ ] Employee self-service portal
- [ ] Overtime tracking and alerts
- [ ] Project-based time tracking
- [ ] Advanced authentication (LDAP, OAuth2)
- [ ] Real-time notifications
- [ ] Compliance reporting
- [ ] Mobile application

## Support

For support, please open an issue on GitHub or contact the maintainers.

---

**Note**: This is a demonstration system. For production use, implement proper security measures, authentication systems, and regular backups.
