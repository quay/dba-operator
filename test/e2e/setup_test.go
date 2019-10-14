package e2e

import (
	"flag"
	"log"
	"os"
	"testing"
)

var (
	testFramework *TestFramework
	operatorImage *string
	dbImage       *string
)

func TestMain(m *testing.M) {
	operatorImage = flag.String(
		"operator-image",
		"",
		"operator image, e.g. quay.io/quay/dba-operator:v1.0.0",
	)
	dbImage = flag.String(
		"db-image",
		"",
		"db image, e.g. mysql/mysql-server:latest",
	)
	flag.Parse()

	var err error
	if testFramework, err = New(*operatorImage); err != nil {
		log.Printf("failed to setup testFramework: %s", err)
		os.Exit(1)
	}

	exitCode := m.Run()
	os.Exit(exitCode)
}
