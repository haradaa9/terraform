package statemgr

import (
	"fmt"

	"github.com/hashicorp/terraform/states/statefile"
)

// Migrator is an optional interface implemented by state managers that
// are capable of direct migration of state snapshots with their associated
// metadata unchanged.
//
// This interface is used when available by function Migrate. See that
// function for more information on how it is used.
type Migrator interface {
	PersistentMeta

	// StateForMigration returns a full statefile representing the latest
	// snapshot (as would be returned by Reader.State) and the associated
	// snapshot metadata (as would be returned by
	// PersistentMeta.StateSnapshotMeta).
	//
	// Just as with Reader.State, this must not fail.
	StateForMigration() *statefile.File

	// WriteStateForMigration accepts a full statefile including associated
	// snapshot metadata and behaves as though Writer.WriteState were called
	// followed by updating the snapshot metadata to match the snapshot.
	WriteStateForMigration(*statefile.File) error
}

// Migrate writes the latest transient state snapshot from src into dest,
// preserving snapshot metadata (serial and lineage) where possible.
//
// If both managers implement the optional interface Migrator then it will
// be used to copy the snapshot and its associated metadata. Otherwise,
// the normal Reader and Writer interfaces will be used instead.
//
// If the destination manager refuses the new state or fails to write it then
// its error is returned directly.
//
// For state managers that also implement Persistent, it is the caller's
// responsibility to persist the newly-written state after a successful result,
// just as with calls to Writer.WriteState.
//
// This function doesn't do any locking of its own, so if the state managers
// also implement Locker the caller should hold a lock on both managers
// for the duration of this call.
func Migrate(dst, src Transient) error {
	if dstM, ok := dst.(Migrator); ok {
		if srcM, ok := src.(Migrator); ok {
			// Full-fidelity migration, them.
			s := srcM.StateForMigration()
			return dstM.WriteStateForMigration(s)
		}
	}

	// Managers to not support full-fidelity migration, so migration will not
	// preserve serial/lineage.
	s := src.State()
	return dst.WriteState(s)
}

// Import loads the given state snapshot into the given manager, preserving
// its metadata (serial and lineage) if the target manager supports metadata.
//
// A state manager must implement the optional interface Migrator to get
// access to the full metadata.
//
// Unless "force" is true, Import will check first that the metadata given
// in the file matches the current snapshot metadata for the manager, if the
// manager supports metadata. Some managers do not support forcing, so a
// write with an unsuitable lineage or serial may still be rejected even if
// "force" is set. "force" has no effect for managers that do not support
// snapshot metadata.
//
// For state managers that also implement Persistent, it is the caller's
// responsibility to persist the newly-written state after a successful result,
// just as with calls to Writer.WriteState.
//
// This function doesn't do any locking of its own, so if the state manager
// also implements Locker the caller should hold a lock on it for the
// duration of this call.
func Import(f *statefile.File, mgr Transient, force bool) error {
	if mgrM, ok := mgr.(Migrator); ok {
		m := mgrM.StateSnapshotMeta()
		if f.Lineage != "" && m.Lineage != "" && !force {
			if f.Lineage != m.Lineage {
				return fmt.Errorf("cannot import state with lineage %q over unrelated state with lineage %q", f.Lineage, m.Lineage)
			}
			if f.Serial == m.Serial {
				currentState := mgr.State()
				if statefile.StatesMarshalEqual(f.State, currentState) {
					// If lineage, serial, and state all match then this is a no-op.
					return nil
				}
				return fmt.Errorf("cannot overwrite existing state with serial %d with a different state that has the same serial", m.Serial)
			} else if f.Serial < m.Serial {
				return fmt.Errorf("cannot import state with serial %d over newer state with lineage %d", f.Serial, m.Serial)
			}
		}
		return mgrM.WriteStateForMigration(f)
	}

	// For managers that don't implement Migrator, this is just a normal write
	// of the state contained in the given file.
	return mgr.WriteState(f.State)
}

// Export retrieves the latest state snapshot from the given manager, including
// its metadata (serial and lineage) where possible.
//
// A state manager must also implement either Migrator or PersistentMeta
// for the metadata to be included. Otherwise, the relevant fields will have
// zero value in the returned object.
//
// For state managers that also implement Persistent, it is the caller's
// responsibility to refresh from persistent storage first if needed.
//
// This function doesn't do any locking of its own, so if the state manager
// also implements Locker the caller should hold a lock on it for the
// duration of this call.
func Export(mgr Reader) *statefile.File {
	switch mgrT := mgr.(type) {
	case Migrator:
		return mgrT.StateForMigration()
	case PersistentMeta:
		s := mgr.State()
		meta := mgrT.StateSnapshotMeta()
		return statefile.New(s, meta.Lineage, meta.Serial)
	default:
		s := mgr.State()
		return statefile.New(s, "", 0)
	}
}