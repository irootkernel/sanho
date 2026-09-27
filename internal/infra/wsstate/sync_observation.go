package wsstate

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// SyncNoteObservation retains the identity and bytes of one opened note.
// It is a bounded observation, not a lock against concurrent writers.
type SyncNoteObservation struct {
	path string
	info os.FileInfo
	data []byte
}

func ObserveSyncNote(gitDir string) (SyncNoteObservation, error) {
	observation := SyncNoteObservation{path: syncNotePath(gitDir)}
	file, err := os.Open(observation.path)
	if os.IsNotExist(err) {
		return observation, nil
	}
	if err != nil {
		return SyncNoteObservation{}, fmt.Errorf("read sync note %s: %w", observation.path, err)
	}
	defer file.Close()
	observation.info, err = file.Stat()
	if err == nil {
		observation.data, err = io.ReadAll(file)
	}
	if err != nil {
		return SyncNoteObservation{}, fmt.Errorf("read sync note %s: %w", observation.path, err)
	}
	return observation, nil
}

func (o SyncNoteObservation) Load() (SyncNote, bool, error) {
	if o.info == nil {
		return SyncNote{}, false, nil
	}
	return decodeSyncNote(o.data, o.path)
}

// Same detects replacement even when a writer preserves the note's contents.
func (o SyncNoteObservation) Same(other SyncNoteObservation) bool {
	if o.info == nil || other.info == nil {
		return o.info == nil && other.info == nil
	}
	return os.SameFile(o.info, other.info) && o.info.ModTime().Equal(other.info.ModTime()) &&
		bytes.Equal(o.data, other.data)
}
