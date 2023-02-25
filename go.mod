module github.com/app-sre/dba-operator

go 1.13

require (
	github.com/DATA-DOG/go-sqlmock v1.3.3
	github.com/deckarep/golang-set v1.7.1
	github.com/go-logr/logr v0.1.0
	github.com/go-sql-driver/mysql v1.4.1
	github.com/gogo/protobuf v1.2.1 // indirect
	github.com/iancoleman/strcase v0.0.0-20190422225806-e506e3ef7365
	github.com/jmoiron/sqlx v1.2.0
	github.com/json-iterator/go v1.1.6 // indirect
	github.com/maxbrunsfeld/counterfeiter/v6 v6.2.2
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.5.0
	github.com/prometheus/client_golang v0.9.0
	github.com/spf13/pflag v1.0.3 // indirect
	github.com/stretchr/testify v1.3.0
	github.com/xo/dburl v0.0.0-20191116074001-eeed741d1a6d
	k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apimachinery v0.15.7
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	sigs.k8s.io/controller-runtime v0.2.1
)
