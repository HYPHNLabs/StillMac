package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

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

// Unprotect removes one validated private exclusion record. A fresh plan is
// still required before an action; this operation never cleans the candidate.
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
	path := filepath.Join(e.cleanupDir(), "protected", id+".json")
	var record protectionRecord
	if err := readStrictJSON(path, &record); err != nil {
		return errors.New("protection changed")
	}
	if record.ID != id || record.SchemaVersion != schemaVersion || !familyMatchesID(record.ID, record.Family) {
		return errors.New("protection changed")
	}
	if err := os.Remove(path); err != nil {
		return errors.New("unable to remove protection")
	}
	return nil
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
