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
// This predicate will skip update events by a ManagedDatabase where its metadata.generation field is not changed,
// unless its Status.CurrentVersion subresource changes. For other object types, update are sent on
// ResourceVersion changes (default).
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

	oldMdb, ok := e.ObjectOld.(*dba.ManagedDatabase)
	if !ok {
		if e.MetaNew.GetResourceVersion() == e.MetaOld.GetResourceVersion() {
			return false
		}
		return true
	}

	newMdb, ok := e.ObjectNew.(*dba.ManagedDatabase)
	if !ok {
		return false
	}

	// Check that the status subresource is enabled, and for currentVersion changes
	// in the ManagedDatabase.
	if e.MetaNew.GetGeneration() > 0 && e.MetaNew.GetGeneration() == e.MetaOld.GetGeneration() {
		if oldMdb.Status.CurrentVersion != newMdb.Status.CurrentVersion {
			return true
		}
		return false
	}

	if e.MetaNew.GetResourceVersion() == e.MetaOld.GetResourceVersion() {
		return false
	}

	return true
}
