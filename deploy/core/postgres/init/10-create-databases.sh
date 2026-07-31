#!/bin/sh
set -eu

psql --set=ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=platform_db="$PLATFORM_DB_NAME" \
  --set=platform_user="$PLATFORM_DB_USER" \
  --set=platform_password="$PLATFORM_DB_PASSWORD" \
  --set=keycloak_db="$KEYCLOAK_DB_NAME" \
  --set=keycloak_user="$KEYCLOAK_DB_USER" \
  --set=keycloak_password="$KEYCLOAK_DB_PASSWORD" <<-'SQL'
  CREATE USER :"platform_user" WITH PASSWORD :'platform_password';
  CREATE DATABASE :"platform_db" OWNER :"platform_user";
  REVOKE ALL ON DATABASE :"platform_db" FROM PUBLIC;

  CREATE USER :"keycloak_user" WITH PASSWORD :'keycloak_password';
  CREATE DATABASE :"keycloak_db" OWNER :"keycloak_user";
  REVOKE ALL ON DATABASE :"keycloak_db" FROM PUBLIC;
SQL

psql --set=ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$PLATFORM_DB_NAME" <<-SQL
  CREATE EXTENSION IF NOT EXISTS pgcrypto;
  CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
SQL
