package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func managedDatabase(generation int64, resourceVersion, currentVersion string) *dba.ManagedDatabase {
	managedDatabase := &dba.ManagedDatabase{
		ObjectMeta: metav1.ObjectMeta{
			ResourceVersion: resourceVersion,
			Generation:      generation,
		},
		Status: dba.ManagedDatabaseStatus{
			CurrentVersion: currentVersion,
		},
	}
	return managedDatabase
}

func job(generation int64, resourceVersion string, active int32) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			ResourceVersion: resourceVersion,
			Generation:      generation,
		},
		Status: batchv1.JobStatus{
			Active: active,
		},
	}
	return job
}

func TestManagedDatabaseVersionChangedPredicate(t *testing.T) {
	mdbPredicate := ManagedDatabaseVersionChangedPredicate{}

	var managedDatabaseUpdatesForPredicateTests = []struct {
		oldGeneration      int
		oldResourceVersion string
		oldCurrentVersion  string
		newGeneration      int
		newResourceVersion string
		newCurrentVersion  string
		expected           bool
	}{
		{0, "1", "v1", 0, "1", "v1", false}, // No-change update
		{1, "1", "v1", 1, "1", "v1", false}, // No-change update w/ generation field set
		{0, "1", "v1", 0, "2", "v2", true},  // Generation not set, currentVersion change (resourceVersion change)
		{0, "1", "v1", 0, "2", "v1", true},  // Generation not set, some update other than currentVersion
		{1, "1", "v1", 1, "2", "v1", false}, // Same generation, same currentVersion (change to some other field)
		{1, "1", "v1", 1, "2", "v2", true},  // currentVersion change, same generation
		{1, "1", "v1", 2, "2", "v1", true},  // Generation change, same currentVersion
		{1, "1", "v1", 2, "2", "v2", true},  // Generation change & currentVersion change
	}

	for _, update := range managedDatabaseUpdatesForPredicateTests {
		oldManagedDatabase := managedDatabase(int64(update.oldGeneration), update.oldResourceVersion, update.oldCurrentVersion)
		newManagedDatabase := managedDatabase(int64(update.newGeneration), update.newResourceVersion, update.newCurrentVersion)

		updateEvent := event.UpdateEvent{
			MetaOld:   &oldManagedDatabase.ObjectMeta,
			ObjectOld: oldManagedDatabase,
			MetaNew:   &newManagedDatabase.ObjectMeta,
			ObjectNew: newManagedDatabase,
		}

		assert.Equal(t, mdbPredicate.Update(updateEvent), update.expected)
	}

	var jobUpdatesForPredicateTests = []struct {
		oldGeneration      int
		oldResourceVersion string
		oldActive          int
		newGeneration      int
		newResourceVersion string
		newActive          int
		expected           bool
	}{
		{0, "1", 0, 0, "1", 0, false}, // No-change update
		{1, "1", 0, 1, "1", 0, false}, // No-change update w/ generation field set
		{0, "1", 0, 0, "2", 1, true},  // Status change, no generation, version update
		{1, "1", 0, 1, "2", 1, true},  // Status change, generation, version update
	}

	// Any other objects other than ManagedDatabase should reconcile unless there is no changes.
	//
	// NOTE: It doesn't look like generation in incremented (or used) for all types by the API server.
	//       For example, as of 1.15, changing the Job types does not make use of the generation field.
	for _, update := range jobUpdatesForPredicateTests {
		oldJob := job(int64(update.oldGeneration), update.oldResourceVersion, int32(update.oldActive))
		newJob := job(int64(update.newGeneration), update.newResourceVersion, int32(update.newActive))

		updateEvent := event.UpdateEvent{
			MetaOld:   &oldJob.ObjectMeta,
			ObjectOld: oldJob,
			MetaNew:   &newJob.ObjectMeta,
			ObjectNew: newJob,
		}

		assert.Equal(t, mdbPredicate.Update(updateEvent), update.expected)
	}
}
