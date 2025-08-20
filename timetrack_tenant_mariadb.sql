-- Multi-Tenant additions for MariaDB/MySQL
CREATE TABLE IF NOT EXISTS `tenants` (
  `id` INT AUTO_INCREMENT PRIMARY KEY,
  `name` VARCHAR(255) NOT NULL,
  `subdomain` VARCHAR(100) UNIQUE NOT NULL,
  `domain` VARCHAR(255),
  `active` TINYINT(1) DEFAULT 1,
  `config` JSON DEFAULT (JSON_OBJECT()),
  `created_at` DATETIME DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Add tenant_id to existing tables
ALTER TABLE `departments` ADD COLUMN IF NOT EXISTS `tenant_id` INT NULL;
ALTER TABLE `users` ADD COLUMN IF NOT EXISTS `tenant_id` INT NULL;
ALTER TABLE `type` ADD COLUMN IF NOT EXISTS `tenant_id` INT NULL;
ALTER TABLE `entries` ADD COLUMN IF NOT EXISTS `tenant_id` INT NULL;

-- FKs
ALTER TABLE `departments` ADD CONSTRAINT `fk_departments_tenant` FOREIGN KEY (`tenant_id`) REFERENCES `tenants`(`id`);
ALTER TABLE `users` ADD CONSTRAINT `fk_users_tenant` FOREIGN KEY (`tenant_id`) REFERENCES `tenants`(`id`);
ALTER TABLE `type` ADD CONSTRAINT `fk_type_tenant` FOREIGN KEY (`tenant_id`) REFERENCES `tenants`(`id`);
ALTER TABLE `entries` ADD CONSTRAINT `fk_entries_tenant` FOREIGN KEY (`tenant_id`) REFERENCES `tenants`(`id`);

-- Indexes
CREATE INDEX IF NOT EXISTS `idx_departments_tenant` ON `departments`(`tenant_id`);
CREATE INDEX IF NOT EXISTS `idx_users_tenant` ON `users`(`tenant_id`);
CREATE INDEX IF NOT EXISTS `idx_type_tenant` ON `type`(`tenant_id`);
CREATE INDEX IF NOT EXISTS `idx_entries_tenant` ON `entries`(`tenant_id`);

-- Default tenant
INSERT INTO `tenants` (`name`, `subdomain`, `domain`, `active`)
SELECT 'Demo Company', 'demo', 'demo.localhost', 1
WHERE NOT EXISTS (SELECT 1 FROM `tenants` WHERE `subdomain` = 'demo');

SET @demoId := (SELECT id FROM `tenants` WHERE `subdomain` = 'demo');
UPDATE `departments` SET `tenant_id` = IFNULL(`tenant_id`, @demoId);
UPDATE `users` SET `tenant_id` = IFNULL(`tenant_id`, @demoId);
UPDATE `type` SET `tenant_id` = IFNULL(`tenant_id`, @demoId);
UPDATE `entries` SET `tenant_id` = IFNULL(`tenant_id`, @demoId);
