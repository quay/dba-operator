package controllers

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	dba "github.com/app-sre/dba-operator/api/v1alpha1"
)

var log = ctrl.Log.WithName("predicate").WithName("eventFilters")

var _ predicate.Predicate = ManagedDatabaseVersionChangedPredicate{}

// ManagedDatabaseVersionChangedPredicate implements an update predicate function on Generation change.
//
// The metadata.generation field of an object is incremented by the API server when writes are made to
// the spec field of an object.  This allows a controller to ignore update events where the spec is unchanged,
// and only the metadata and/or status fields are changed.
//
// This predicate will skip update events that have no change in the object's metadata.generation field,
// unless it is a ManagedDatabase, and its Status.CurrentVersion subresource changes.
type ManagedDatabaseVersionChangedPredicate struct {
	predicate.Funcs
}

// Update implements filter for validating generation change
func (ManagedDatabaseVersionChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.MetaOld == nil {
		log.Error(nil, "Update event has no old metadata", "event", e)
		return false
	}
	if e.ObjectOld == nil {
		log.Error(nil, "Update event has no old runtime object to update", "event", e)
		return false
	}
	if e.ObjectNew == nil {
		log.Error(nil, "Update event has no new runtime object for update", "event", e)
		return false
	}
	if e.MetaNew == nil {
		log.Error(nil, "Update event has no new metadata", "event", e)
		return false
	}
	// Check that the status subresource is enabled, and for currentVersion changes
	// in the ManagedDatabase.
	if e.MetaNew.GetGeneration() > 0 && e.MetaNew.GetGeneration() == e.MetaOld.GetGeneration() {
		oldMdb, ok := e.ObjectOld.(*dba.ManagedDatabase)
		if !ok {
			return false
		}

		newMdb, ok := e.ObjectNew.(*dba.ManagedDatabase)
		if !ok {
			return false
		}

		if oldMdb.Status.CurrentVersion != newMdb.Status.CurrentVersion {
			return true
		}
		return false
	}
	return true
}
