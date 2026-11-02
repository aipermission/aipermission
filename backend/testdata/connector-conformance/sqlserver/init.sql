CREATE DATABASE aipermission;
GO
USE aipermission;
CREATE TABLE dbo.events (id bigint NOT NULL, label nvarchar(100), amount decimal(38,6), payload varbinary(8));
INSERT dbo.events VALUES (9223372036854775807, N'fixture-sqlserver', 12345678901234567890.123456, 0xFF0012), (2,N'second',0.1,NULL);
CREATE TABLE dbo.[order details] (second_column int, id int);
CREATE TABLE dbo.[name]]edge] (id int);
CREATE TABLE dbo.[quote"edge] ([column"name] int);
INSERT dbo.[quote"edge] VALUES (17);
CREATE TABLE dbo.large_values (value nvarchar(max));
INSERT dbo.large_values SELECT REPLICATE(CONVERT(nvarchar(max),N'a'),70000);
CREATE TABLE dbo.policy_canary (value int NOT NULL);
INSERT dbo.policy_canary VALUES (1);
CREATE TYPE dbo.fixture_integer FROM int;
GO
CREATE TABLE dbo.result_types (date_value date, time_value time(7), datetime_value datetime,
 smalldatetime_value smalldatetime, datetime2_value datetime2(7), offset_value datetimeoffset(7),
 variant_value sql_variant, xml_value xml, spatial_value geography, safe_alias dbo.fixture_integer);
INSERT dbo.result_types VALUES ('2026-11-02','13:14:15.1234567','2026-11-02T13:14:15',
 '2026-11-02T13:14:00','2026-11-02T13:14:15.1234567','2026-11-02T13:14:15.1234567+05:30',
 CAST(123.4500 AS decimal(12,4)),CONVERT(xml,N'<root>'+REPLICATE(CONVERT(nvarchar(max),N'a'),150000)+N'</root>'),geography::Point(41,29,4326),17);
CREATE SEQUENCE dbo.fixture_sequence AS bigint START WITH 1;
GO
CREATE FUNCTION dbo.unreviewed() RETURNS int AS BEGIN RETURN 1 END;
GO
CREATE LOGIN fixture_reader WITH PASSWORD='Fixture-only-Password1!', CHECK_POLICY=OFF;
CREATE USER fixture_reader FOR LOGIN fixture_reader;
ALTER ROLE db_datareader ADD MEMBER fixture_reader;
DENY INSERT, UPDATE, DELETE, EXECUTE, ALTER TO fixture_reader;
CREATE LOGIN fixture_policy_writer WITH PASSWORD='Fixture-only-Policy1!', CHECK_POLICY=OFF;
CREATE USER fixture_policy_writer FOR LOGIN fixture_policy_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON dbo.policy_canary TO fixture_policy_writer;
GO
