# Database Operator

This project will provide a an interface for automating traditionally DBA managed aspects of using a relational database, e.g. migrations, access control, and health.

## Development Environment

### Pre-requisities

1. This project is based off of [kubebuilder](https://github.com/kubernetes-sigs/kubebuilder) so it and all of it's dependencies must be installed using the kubebuilder [installation instructions](https://book.kubebuilder.io/quick-start.html#installation).
1. Access to a local kubernetes cluster ([minikube](https://github.com/kubernetes/minikube), [docker for mac](https://docs.docker.com/docker-for-mac/install/)) with the `KUBECFG` env var set properly.
1. [kubefwd](https://github.com/txn2/kubefwd) to get access to services running on cluster and to use stable service names from the dev env and cluster.

### Dev environment

```sh
make devenv
sudo kubefwd svc -n default
docker build -t quay.io/quaymigrations/test deploy/examples/migrationcontainer/
make run
```

## Design

This project will use the operator pattern, and a set of requirements about defining and packaging schema migrations, data migrations, and user access.

### Interface

* CRD called DatabaseMigration
  * Reference to a backfill/data migration container
    * Define allowable parallelism for migration container
  * Reference to the name of the previous DatabaseMigration that is being deprecated
  * A set of hints about what the migration container will do to the database schema
* CRD called ManagedDatabase
  * Reference to a secret which contains admin credentials
  * Desired schema version
  * Config for how the migration will signal its completion in the database
    * e.g. alembic writes a value to a table called `alembic_version`
  * Descriptions of the data metrics that the operator should harvest and publish from the managed DB.

### Flow

1. Developer creates a migration container conforming to the [migration container spec](deploy/examples/migrationcontainer/README.md)
1. Developer creates a DatabaseMigration that describes and references the migraiton container
  1. For some applications (e.g. Quay) there is tooling to generate this from the migration itself
1. Developer generates versions of the app which use the schema and credentials at the corresponding migration version
1. Operator loads the DatabaseMigration into a cluster
1. Operator updates the desired database schema version in the ManagedDatabase CR

### Operator Control Loop

1. Read and publish metrics from the ManagedDatabase CR
1. Check if database schema version matches the desired version
  1. If newer, run through all required migration loops until version matches desired

#### Migration Loop

1. Verify that credentials from 2-migrations ago are unused
  1. Ensures that the code which is incompatible with the change we're about to make is no longer accessing the database using a published secret
  1. Ensure there are no existing connections using the credentials about to be dropped
1. Drop credentials from 2-migrations ago
1. Run database migration if required
1. Generate and add credentials for accessing the database from this migration version 
  1. Write secret containing the password that was generated

### Hints

The following are desired scenarios that we aim to be able to detect via hints
and running database metadata.

1. Applying a blocking index creation to a table that is large
1. Applying a blocking index creation to a table that has heavy writes
1. Adding a column to a table that is large ✔
1. Adding a column to a table that has heavy writes
1. Adding a unique constraint to a table which has non-unique values ✔
1. Adding a constraint to a column on a table that is large
1. Adding a constraint to a column on a table that has heavy writes
1. Making a column non-null when the database existing nulls ✔
1. Adding a non-null column without providing a server default on a table that already has data ✔

### FAQs

#### Why report success or failure to prometheus when the job status has that information already?

This will allow us to keep metrics on how often migrations are passing or failing. In aggregate
this could allow one to manage a large number of databases statistically.

#### Is it desirable to alert based on a failed migration? What does the SOP look like in that case?

It may be desirable to inform an SRE immediately if certain migrations
have failed. The SOP would probably look like:

1. Get paged
2. Read the status block in the `ManagedDatabase` object to find out what failed and when
3. Debug or call support

#### What happens when the database is rolled back to an earlier version via a backup?

The rectification loop will see that the desired version is older than the current version
and start the migration process again.

Open Question: should we clean up any credentials that exist for mifrations past the current version
shown by the backup?

#### What happens when a migration container fails? Do we rollback automatically?

We report the status back via prometheus, which can then be alerted on, and write a corresponding
status line in the k8s job. We will not rollback automatically.

#### What happens when a migration is running and prometheus or prom push gateway die?

We will page if prometheus is offline. We will page if prometheus can't scrape the push gateway.
In no cases should the operator modify the status of the `Job` object based on an inability to read
status/metrics.

#### How do we get the version specific app credentials to the application servers?

TBD

#### Can we ask K8s if anyone is using the app credentials before rolling them?

TBD

#### What internal state does the operator keep? What happens when it crashes or is upgraded?

TBD

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
