CREATE ROLE compliance_owner LOGIN PASSWORD 'owner_dev_pw';
CREATE ROLE compliance_app LOGIN PASSWORD 'app_dev_pw' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
CREATE DATABASE compliance OWNER compliance_owner;
CREATE ROLE zitadel LOGIN PASSWORD 'zitadel_dev_pw' CREATEDB;
CREATE DATABASE zitadel OWNER zitadel;
\connect compliance
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO compliance_owner;
GRANT USAGE ON SCHEMA public TO compliance_app;
REVOKE ALL ON DATABASE compliance FROM PUBLIC;
GRANT CONNECT ON DATABASE compliance TO compliance_owner, compliance_app;
REVOKE ALL ON DATABASE zitadel FROM PUBLIC;
GRANT CONNECT, CREATE, TEMPORARY ON DATABASE zitadel TO zitadel;
