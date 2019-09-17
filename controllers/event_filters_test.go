package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var managedDatabaseUpdatesForPredicateTests = []struct {
	oldGeneration     int
	oldCurrentVersion string
	newGeneration     int
	newCurrentVersion string
	expected          bool
}{
	{0, "v1", 0, "v2", true},  // Status subresource not enabled, currentVersion change
	{0, "v1", 0, "v1", true},  // Status subresource not enabled, some update other than currentVersion
	{1, "v1", 1, "v1", false}, // Same generation, same currentVersion (change to some other field)
	{1, "v1", 1, "v2", true},  // currentVersion change, same generation
	{1, "v1", 2, "v1", true},  // Generation change, same currentVersion
	{1, "v1", 2, "v2", true},  // Generation change & version change
}

func managedDatabase(generation int64, currentVersion string) *dba.ManagedDatabase {
	managedDatabase := &dba.ManagedDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Generation: generation,
		},
		Status: dba.ManagedDatabaseStatus{
			CurrentVersion: currentVersion,
		},
	}
	return managedDatabase
}

func TestManagedDatabaseVersionChangedPredicate(t *testing.T) {
	mdbPredicate := ManagedDatabaseVersionChangedPredicate{}

	for _, update := range managedDatabaseUpdatesForPredicateTests {
		oldManagedDatabase := managedDatabase(int64(update.oldGeneration), update.oldCurrentVersion)
		newManagedDatabase := managedDatabase(int64(update.newGeneration), update.newCurrentVersion)

		updateEvent := event.UpdateEvent{
			MetaOld:   &oldManagedDatabase.ObjectMeta,
			ObjectOld: oldManagedDatabase,
			MetaNew:   &newManagedDatabase.ObjectMeta,
			ObjectNew: newManagedDatabase,
		}

		assert.Equal(t, mdbPredicate.Update(updateEvent), update.expected)
	}
}
