-- Desired schema for mssqldef example
CREATE TABLE dbo.users (
    id INT IDENTITY(1,1) PRIMARY KEY,
    name NVARCHAR(255) NOT NULL,
    email NVARCHAR(320) NOT NULL
);

CREATE INDEX IX_users_email ON dbo.users(email);
