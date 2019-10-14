package e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/app-sre/dba-operator/pkg/dbadmin"
)

var ()

type finalizerFn func() error

type TestFramework struct {
	client.Client
	admin dbadmin.DbAdmin
}

func New(operatorImage string) (*TestFramework, error) {
	c, err := client.New(config.GetConfigOrDie(), client.Options{})
	if err != nil {
		return nil, nil
	}

	return &TestFramework{
		Client: c,
	}, nil
}

// Create a new test context with the test's name and the current time as its ID.
// The ID will be used for creating objects, such as creating a namespace for the test.
func (tf *TestFramework) NewTestCtx(t *testing.T) TestCtx {
	prefix := strings.TrimPrefix(
		strings.ReplaceAll(
			strings.ToLower(t.Name()),
			"/",
			"-",
		),
		"test",
	)

	id := prefix + "-" + strconv.FormatInt(time.Now().Unix(), 36)
	return TestCtx{
		ID: id,
	}
}

type TestCtx struct {
	ID         string
	cleanupFns []finalizerFn
	ctx        context.Context
}

func (tctx *TestCtx) Cleanup(t *testing.T) {
	var eg errgroup.Group

	for i := len(tctx.cleanupFns) - 1; i >= 0; i-- {
		eg.Go(tctx.cleanupFns[i])
	}

	if err := eg.Wait(); err != nil {
		t.Fatal(err)
	}
}

func (tctx *TestCtx) AddFinalizerFn(fn finalizerFn) {
	tctx.cleanupFns = append(tctx.cleanupFns, fn)
}

func (tctx *TestCtx) CreateNamespace(t *testing.T, c client.Client) string {
	name := tctx.ID
	if err := CreateNamespace(tctx.ctx, c, name); err != nil {
		t.Fatal(err)
	}

	namespaceCleanupFn := func() error {
		return DeleteNamespace(tctx.ctx, c, name)
	}

	tctx.AddFinalizerFn(namespaceCleanupFn)

	return name
}

func (tctx *TestCtx) CreateDbaOperator(c client.Client, operatorImage string) error {
	deployment, err := MakeDeployment("../../deploy/dba-operator.yaml")
	if err != nil {
		return err
	}

	if operatorImage != "" {
		repoTag := strings.Split(operatorImage, ":")
		if len(repoTag) != 2 {
			return fmt.Errorf("invalid operator image '%s'", operatorImage)
		}

		deployment.Spec.Template.Spec.Containers[0].Image = operatorImage
	}

	err = CreateDeployment(tctx.ctx, c, tctx.ID, deployment)
	if err != nil {
		return err
	}

	return nil
}

func (tctx *TestCtx) CreateDB(c client.Client, image string) error {
	deployment, err := MakeDeployment("../../deploy/debug.yaml")
	if err != nil {
		return err
	}

	if image != "" {
		repoTag := strings.Split(image, ":")
		if len(repoTag) != 2 {
			return fmt.Errorf("invalid operator image '%s'", image)
		}

		deployment.Spec.Template.Spec.Containers[0].Image = image
	}

	err = CreateDeployment(tctx.ctx, c, tctx.ID, deployment)
	if err != nil {
		return err
	}

	return nil
}
