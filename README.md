# Database Operator

This project will provide a an interface for automating traditionally DBA managed aspects of using a relational database, e.g. migrations, access control, and health.

## Design

This project will use the operator pattern, and a set of requirements about defining and packaging schema migrations, data migrations, and user access.

### Interface

* CRD called AppDatabaseMigration
  * Contains a list of phases:
    * DSL for migration to add "new" schema
    * Reference to a backfill/data migration container
      * Define allowable parallelism for migration container
    * Name of secret under which credentials will be published 
  * Link to the AppDatabaseMigration being deprecated?
* CRD called ManagedDatabase
  * Reference to a secret which contains admin credentials
  * Connection URI
  * Desired schema version

### Flow

1. Developer proposes a schema change
  1. If modeled in peewee, scaffolding proposes the AppDatabaseMigration with the schema migrations pre-filled
  1. Developer can optionally hand-write the AppDatabaseMigration
1. Developer writes the data migration code and packages it as a container
  1. Progress must conform to the progress interface (e.g. progress varz)
1. Developer generates versions of the app which use the credentials named in the AppDatabaseMigration and use the schema at the corresponding phase points
1. Operator loads the AppDatabaseMigration into a cluster
1. Operator updates the desired database schema version in the ManagedDatabase CR

### Operator Control Loop

1. Check if database schema version matches the desired version
  1. If newer, run through all required migration phase loops until version matches desired

#### Phase Loop

1. Drop credentials from 2-phases ago
  1. Ensures that the code which is incompatible with the change we're about to make is no longer accessing the database
1. Run database schema migration if specified
1. Run data backfill migration if specified
1. Generate and add credentials for accessing the database from this phase 
  1. Write secret containing the password that was generated

### Notes

* DSL for describing the schema migration itself
  * Model in peewee?
  * Use [Liquibase](https://rollout.io/blog/liquibase-tutorial-manage-database-schema/) format?
  * Use something that can be generated from an alembic migration?
* Data migrations
  * Python?
  * Container based language independent?
* Access control
  * Automatically generate 4 sets of credentials
    * How to communicate them to the app?
      * CRD status block?
      * Secret with a name?
    * Credentials must not be shared in the app code that gets distributed to customers
      * Shared username, password stored in secret named after the username?
    * How to prevent the app from not switching to the app specific credentials and just using admin credentials
      * Only give the DB operator the admin credentials 
* Migration models
  * Up/down containers
    * Multiple backends can be created easily, e.g. alembic for sqlalchemy projects
    * Less possibility for feature creep
    * Stories that will be harder
      * Migration failure visibility
      * App to schema mapping i.e. how to force provisioning/deprovisioning of credentials
  * DSL
    * Tooling around the content of the migrations as matched with running database
      * Adding a column on a table with a lot of write pressure will be bad
      * Adding an index without background indexing will lock the table
    * Stories that will be harder
      * All of them, since there is an extra layer
  * Hints
    * Keep the migrations in a backend specific container
    * Put some hints about what will be done to the tables in the migration CR
      * Adding an index to a table
        * MySQL implementation will check the DB to see if that will block
      * Adding a unique index to a table
        * MySQL implementation will check the data and warn if it will fail and warn that it will block writes
      * Adding a column to a table
        * MySQL implementation will check the table size and warn if it should use pt-schema-migrate, and that it may block

## Previous Work

* [pt-online-schema-change](https://www.percona.com/doc/percona-toolkit/LATEST/pt-online-schema-change.html)
* [Liquibase](https://rollout.io/blog/liquibase-tutorial-manage-database-schema/)
* [MySQL Online DDL](https://www.fromdual.com/online-ddl_vs_pt-online-schema-change)
* [Facebook OSC](https://github.com/facebookincubator/OnlineSchemaChange)
* [Quay 4-Phase Migrations](https://github.com/coreos-inc/quay-policies-encrypted/blob/master/dbmigrations.md)

