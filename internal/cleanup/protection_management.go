package cleanup

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

const protectionGenerationFile = "protection-generation.json"

type protectionGenerationRecord struct {
	SchemaVersion string `json:"schema_version"`
	Generation    uint64 `json:"generation"`
}

// Protection identifies a persisted exclusion without disclosing its private path.
type Protection struct {
	ID     string `json:"id"`
	Family string `json:"family"`
}

// Protections reads existing exclusions without creating state.
func (e *Engine) Protections() ([]Protection, error) {
	if err := e.validateProtectionParents(); err != nil {
		return nil, err
	}
	if _, err := readProtectionGeneration(e.cleanupDir()); err != nil {
		return nil, errors.New("protection state unavailable")
	}
	items, err := readProtected(e.cleanupDir())
	if err != nil {
		return nil, errors.New("protection state unavailable")
	}
	rows := make([]Protection, 0, len(items))
	for id, family := range items {
		rows = append(rows, Protection{ID: id, Family: family})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

// Unprotect marks one validated private exclusion record inactive. A fresh
// plan is still required before an action; this operation never cleans the candidate.
func (e *Engine) Unprotect(id string) error {
	if !validCandidateID(id) {
		return errors.New("invalid candidate ID")
	}
	rows, err := e.Protections()
	if err != nil {
		return err
	}
	found := false
	for _, row := range rows {
		if row.ID == id {
			found = true
		}
	}
	if !found {
		return errors.New("candidate is not protected")
	}
	if err := e.validateProtectionParents(); err != nil {
		return err
	}
	protectedDir := filepath.Join(e.cleanupDir(), "protected")
	file, err := openProtectionRecord(protectedDir, id)
	if err != nil {
		return errors.New("protection changed")
	}
	defer file.Close()
	var record protectionRecord
	if err := readProtectionRecordFile(file, &record); err != nil {
		return errors.New("protection changed")
	}
	if record.ID != id || record.SchemaVersion != schemaVersion || !familyMatchesID(record.ID, record.Family) || (record.State != "" && record.State != "protected") {
		return errors.New("protection changed")
	}
	if err := advanceProtectionGeneration(e.cleanupDir()); err != nil {
		return errors.New("protection changed")
	}
	record.State = "unprotected"
	if err := writeProtectionRecord(file, record); err != nil {
		return errors.New("unable to remove protection")
	}
	return nil
}

func readProtectionGeneration(cleanupDir string) (uint64, error) {
	var record protectionGenerationRecord
	path := filepath.Join(cleanupDir, protectionGenerationFile)
	if err := readStrictJSON(path, &record); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	if record.SchemaVersion != schemaVersion {
		return 0, errors.New("malformed protection generation")
	}
	return record.Generation, nil
}

func advanceProtectionGeneration(cleanupDir string) error {
	generation, err := readProtectionGeneration(cleanupDir)
	if err != nil {
		return err
	}
	if generation == ^uint64(0) {
		return errors.New("protection generation exhausted")
	}
	return atomicJSON(filepath.Join(cleanupDir, protectionGenerationFile), protectionGenerationRecord{SchemaVersion: schemaVersion, Generation: generation + 1})
}

func openProtectionRecord(protectedDir, id string) (*os.File, error) {
	if !validCandidateID(id) {
		return nil, errors.New("invalid candidate ID")
	}
	parent, err := os.Lstat(protectedDir)
	if err != nil || parent.Mode()&os.ModeSymlink != 0 || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		if err == nil {
			err = errors.New("unsafe protection directory")
		}
		return nil, err
	}
	parentDevice, parentInode, err := identityInfo(parent)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(protectedDir, id+".json")
	expected, err := os.Lstat(path)
	if err != nil || expected.Mode()&os.ModeSymlink != 0 || !expected.Mode().IsRegular() || expected.Mode().Perm() != 0600 {
		if err == nil {
			err = errors.New("unsafe protection record")
		}
		return nil, err
	}
	if err := validateProtectionRecordOwnership(expected); err != nil {
		return nil, err
	}
	expectedDevice, expectedInode, err := identityInfo(expected)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, errors.New("unable to open protection record")
	}
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || actual.Mode().Perm() != 0600 {
		if err == nil {
			err = errors.New("unsafe protection record")
		}
		_ = file.Close()
		return nil, err
	}
	if err := validateProtectionRecordOwnership(actual); err != nil {
		_ = file.Close()
		return nil, err
	}
	actualDevice, actualInode, err := identityInfo(actual)
	if err != nil || actualDevice != expectedDevice || actualInode != expectedInode {
		if err == nil {
			err = errors.New("protection record changed")
		}
		_ = file.Close()
		return nil, err
	}
	currentParent, err := os.Lstat(protectedDir)
	if err != nil || currentParent.Mode()&os.ModeSymlink != 0 || !currentParent.IsDir() || currentParent.Mode().Perm() != 0700 {
		if err == nil {
			err = errors.New("unsafe protection directory")
		}
		_ = file.Close()
		return nil, err
	}
	currentParentDevice, currentParentInode, err := identityInfo(currentParent)
	if err != nil || currentParentDevice != parentDevice || currentParentInode != parentInode {
		if err == nil {
			err = errors.New("protection directory changed")
		}
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateProtectionRecordOwnership(info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("unsupported protection record identity")
	}
	if st.Nlink != 1 || uint32(st.Uid) != uint32(os.Getuid()) {
		return errors.New("unsafe protection record ownership")
	}
	return nil
}

func readProtectionRecordFile(file *os.File, record *protectionRecord) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(record); err != nil {
		return errors.New("malformed protection")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("malformed protection")
	}
	return nil
}

func writeProtectionRecord(file *os.File, record protectionRecord) error {
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Write(b); err != nil {
		return err
	}
	return file.Sync()
}

func (e *Engine) validateProtectionParents() error {
	if e.Config.DataDir == "" {
		return errors.New("data directory unavailable")
	}
	if err := validateCreationPath(e.Config.DataDir); err != nil {
		return errors.New("unsafe protection state")
	}
	for _, path := range []string{e.Config.DataDir, e.cleanupDir(), filepath.Join(e.cleanupDir(), "protected")} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
			return errors.New("unsafe protection state")
		}
		if _, _, err := identityInfo(info); err != nil {
			return errors.New("unsafe protection state")
		}
	}
	return nil
}
