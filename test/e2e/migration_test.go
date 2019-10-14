package e2e

import "testing"

func TestBasicMigration(t *testing.T) {
	ctx := testFramework.NewTestCtx(t)
	// defer ctx.Cleanup(t)

	ctx.CreateNamespace(t, testFramework.Client)

	err := ctx.CreateDB(testFramework.Client, *dbImage)
	if err != nil {
		t.Fatal(err)
	}

	err = ctx.CreateDbaOperator(testFramework.Client, *operatorImage)
	if err != nil {
		t.Fatal(err)
	}

	// TODO
}
