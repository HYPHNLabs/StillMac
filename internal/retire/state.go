package retire

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

func (e *Engine) retireDir() string {
	return filepath.Join(e.Config.DataDir, "retire")
}

func (e *Engine) statePath(kind, id string) string {
	return filepath.Join(e.retireDir(), kind, id+".json")
}

func (e *Engine) ensureState() error {
	if e.Config.DataDir == "" {
		return fail(CodeState, "private data directory unavailable")
	}
	dataDir, err := absoluteCleanPath(e.Config.DataDir)
	if err != nil {
		return fail(CodeState, "private data directory is unsafe")
	}
	e.Config.DataDir = dataDir
	for _, path := range []string{
		dataDir,
		e.retireDir(),
		filepath.Join(e.retireDir(), "registrations"),
		filepath.Join(e.retireDir(), "plans"),
		filepath.Join(e.retireDir(), "targets"),
		filepath.Join(e.retireDir(), "approvals"),
		filepath.Join(e.retireDir(), "receipts"),
		filepath.Join(e.retireDir(), "protected"),
	} {
		if err := ensurePrivateDir(path); err != nil {
			return fail(CodeState, "private state directory is unsafe")
		}
	}
	return validateState(e.retireDir())
}

func ensurePrivateDir(path string) error {
	path, err := absoluteCleanPath(path)
	if err != nil {
		return err
	}
	info, statErr := os.Lstat(path)
	if statErr == nil {
		return privateDirectory(info)
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	parent := filepath.Dir(path)
	if parent == path {
		return errors.New("private directory parent unavailable")
	}
	if err := ensureStateParent(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("private directory creation raced")
		}
		return err
	}
	info, err = os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("unsafe private directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil {
		return err
	}
	return privateDirectory(info)
}

func ensureStateParent(path string) error {
	path, err := absoluteCleanPath(path)
	if err != nil {
		return err
	}
	info, statErr := os.Lstat(path)
	if statErr == nil {
		return trustedDirectory(info)
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	parent := filepath.Dir(path)
	if parent == path {
		return errors.New("private state parent unavailable")
	}
	if err := ensureStateParent(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("private state parent creation raced")
		}
		return err
	}
	info, err = os.Lstat(path)
	if err != nil {
		return err
	}
	return trustedDirectory(info)
}

func trustedDirectory(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return errors.New("unsafe private state parent")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("private state ownership unavailable")
	}
	uid := uint32(os.Getuid())
	if st.Uid != uid && st.Uid != 0 {
		return errors.New("private state ownership unavailable")
	}
	return nil
}

func privateDirectory(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("unsafe private directory")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) {
		return errors.New("private directory ownership unavailable")
	}
	return nil
}

func validateState(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return errors.New("unsafe private state root")
	}
	if err := privateDirectory(info); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	allowed := map[string]bool{
		"registrations": true,
		"plans":         true,
		"targets":       true,
		"approvals":     true,
		"receipts":      true,
		"protected":     true,
		"host.json":     true,
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return errors.New("unknown private state entry")
		}
		if entry.Name() == "host.json" {
			if err := validatePrivateFile(filepath.Join(root, entry.Name()), false); err != nil {
				return err
			}
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return errors.New("unsafe private state directory")
		}
		if info, err := entry.Info(); err != nil {
			return errors.New("unsafe private state directory permissions")
		} else if err := privateDirectory(info); err != nil {
			return err
		}
		if err := validateStateFiles(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func validateStateFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			return errors.New("unsafe private state entry")
		}
		if err := validatePrivateFile(filepath.Join(dir, entry.Name()), true); err != nil {
			return err
		}
	}
	return nil
}

func validatePrivateFile(path string, requireJSON bool) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("unsafe private state file")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) {
		return errors.New("private state file ownership unavailable")
	}
	if requireJSON && filepath.Ext(path) != ".json" {
		return errors.New("unsafe private state file")
	}
	return nil
}

func atomicJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.OpenFile(filepath.Join(dir, ".stillmac-retire-tmp"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(b)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	closed = true
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.New("unsafe private state destination")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func readStrictJSON(path string, value any) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	if err := validatePrivateFile(path, true); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > 4<<20 {
		return errors.New("private state file too large")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("malformed private state")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("malformed private state")
	}
	return nil
}

func (e *Engine) hostID() (string, error) {
	if e.Config.HostID != "" {
		if len(e.Config.HostID) < 1 || strings.IndexByte(e.Config.HostID, 0) >= 0 {
			return "", fail(CodeInvalidRequest, "host identity is invalid")
		}
		return e.Config.HostID, nil
	}
	path := filepath.Join(e.retireDir(), "host.json")
	var record hostRecord
	if err := readStrictJSON(path, &record); err == nil {
		if record.SchemaVersion != SchemaVersion || len(record.ID) != 32 || !isLowerHex(record.ID) {
			return "", fail(CodeState, "host identity is invalid")
		}
		return record.ID, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fail(CodeState, "host identity is invalid")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fail(CodeState, "host identity unavailable")
	}
	record = hostRecord{SchemaVersion: SchemaVersion, ID: hexString(random)}
	if err := atomicJSON(path, record); err != nil {
		return "", fail(CodeState, "host identity unavailable")
	}
	return record.ID, nil
}

func hexString(value []byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for i, b := range value {
		result[i*2] = digits[b>>4]
		result[i*2+1] = digits[b&0x0f]
	}
	return string(result)
}

func isLowerHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func registrationID(path string) string {
	return "retire-" + opaqueHash(path)[:32]
}

func registrationHash(reg privateRegistration) string {
	reg.Hash = ""
	b, _ := json.Marshal(reg)
	return opaqueHash(string(b))
}

func targetHash(target privateTarget) string {
	target.Hash = ""
	target.PlanID = ""
	b, _ := json.Marshal(target)
	return opaqueHash(string(b))
}

func approvalHash(approval privateApproval) string {
	approval.Hash = ""
	b, _ := json.Marshal(approval)
	return opaqueHash(string(b))
}

func protectionHash(protection privateProtection) string {
	protection.Hash = ""
	b, _ := json.Marshal(protection)
	return opaqueHash(string(b))
}

func receiptHash(receipt privateReceipt) string {
	receipt.Hash = ""
	b, _ := json.Marshal(receipt)
	return opaqueHash(string(b))
}

func planHash(plan Plan) string {
	plan.PlanID = ""
	plan.PlanHash = ""
	b, _ := json.Marshal(plan)
	return opaqueHash(string(b))
}

func (e *Engine) writeRegistration(reg privateRegistration) error {
	reg.Hash = registrationHash(reg)
	return atomicJSON(e.statePath("registrations", reg.RegistrationID), reg)
}

func (e *Engine) loadRegistration(id string) (privateRegistration, error) {
	var reg privateRegistration
	if !validID(id, "retire-") {
		return reg, fail(CodeInvalidRequest, "registration ID is invalid")
	}
	if err := readStrictJSON(e.statePath("registrations", id), &reg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return reg, fail(CodeNotFound, "registration not found")
		}
		return reg, fail(CodeTampered, "registration state is invalid")
	}
	if reg.SchemaVersion != SchemaVersion || reg.RegistrationID != id || reg.Hash != registrationHash(reg) {
		return reg, fail(CodeTampered, "registration state is invalid")
	}
	if _, err := absoluteCleanPath(reg.TargetPath); err != nil || (reg.MainRoot != "" && !filepath.IsAbs(reg.MainRoot)) {
		return reg, fail(CodeTampered, "registration state is invalid")
	}
	return reg, nil
}

func (e *Engine) loadPlan(id string) (Plan, privateTarget, error) {
	var plan Plan
	var target privateTarget
	if !validID(id, "retire-plan-") {
		return plan, target, fail(CodeInvalidRequest, "plan ID is invalid")
	}
	if err := readStrictJSON(e.statePath("plans", id), &plan); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return plan, target, fail(CodeNotFound, "plan not found")
		}
		return plan, target, fail(CodeTampered, "plan state is invalid")
	}
	if err := readStrictJSON(e.statePath("targets", id), &target); err != nil {
		return plan, target, fail(CodeTampered, "target registry is invalid")
	}
	if plan.SchemaVersion != SchemaVersion || plan.PlanID != id || plan.RuleSet != RuleSetVersion || plan.PlanHash != planHash(plan) || id != "retire-plan-"+plan.PlanHash[:16] {
		return plan, target, fail(CodeTampered, "plan state is invalid")
	}
	if target.SchemaVersion != SchemaVersion || target.PlanID != id || target.RegistrationID != plan.RegistrationID || target.Hash != targetHash(target) || plan.TargetRegistryHash != target.Hash || target.RegistrationHash != target.Registration.Hash {
		return plan, target, fail(CodeTampered, "target registry is invalid")
	}
	if target.Registration.SchemaVersion != SchemaVersion || target.Registration.RegistrationID != plan.RegistrationID || target.Registration.Hash != registrationHash(target.Registration) {
		return plan, target, fail(CodeTampered, "target registry is invalid")
	}
	return plan, target, nil
}

func (e *Engine) loadApproval(id string) (privateApproval, error) {
	var approval privateApproval
	if !validID(id, "retire-plan-") {
		return approval, fail(CodeInvalidRequest, "plan ID is invalid")
	}
	if err := readStrictJSON(e.statePath("approvals", id), &approval); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return approval, fail(CodeBlocked, "plan approval is required")
		}
		return approval, fail(CodeTampered, "plan approval is invalid")
	}
	if approval.SchemaVersion != SchemaVersion || approval.PlanID != id || approval.Hash != approvalHash(approval) {
		return approval, fail(CodeTampered, "plan approval is invalid")
	}
	return approval, nil
}

func (e *Engine) protection(id string) (bool, error) {
	if !validID(id, "retire-") {
		return false, fail(CodeInvalidRequest, "registration ID is invalid")
	}
	var record privateProtection
	if err := readStrictJSON(e.statePath("protected", id), &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fail(CodeTampered, "protection state is invalid")
	}
	if record.SchemaVersion != SchemaVersion || record.ID != id || record.Hash != protectionHash(record) {
		return false, fail(CodeTampered, "protection state is invalid")
	}
	return record.Protected, nil
}

func (e *Engine) writeProtection(id string, protected bool) error {
	record := privateProtection{SchemaVersion: SchemaVersion, ID: id, Protected: protected}
	record.Hash = protectionHash(record)
	return atomicJSON(e.statePath("protected", id), record)
}

func (e *Engine) writeReceipt(private privateReceipt) error {
	private.Hash = receiptHash(private)
	return atomicJSON(e.statePath("receipts", private.Receipt.ReceiptID), private)
}

func (e *Engine) loadReceipt(id string) (privateReceipt, error) {
	var receipt privateReceipt
	if !validID(id, "retire-receipt-") && !validID(id, "retire-recover-") {
		return receipt, fail(CodeInvalidRequest, "receipt ID is invalid")
	}
	if err := readStrictJSON(e.statePath("receipts", id), &receipt); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return receipt, fail(CodeNotFound, "receipt not found")
		}
		return receipt, fail(CodeTampered, "receipt state is invalid")
	}
	if receipt.SchemaVersion != SchemaVersion || receipt.Receipt.ReceiptID != id || receipt.Hash != receiptHash(receipt) || !validReceipt(receipt.Receipt) {
		return receipt, fail(CodeTampered, "receipt state is invalid")
	}
	return receipt, nil
}

func validReceipt(receipt Receipt) bool {
	if receipt.SchemaVersion != SchemaVersion || !validID(receipt.ReceiptID, "retire-receipt-") && !validID(receipt.ReceiptID, "retire-recover-") || receipt.RegistrationID == "" || receipt.Timestamp == "" || !validCommitOptional(receipt.RetainedCommit) {
		return false
	}
	if receipt.Operation != OperationRetire && receipt.Operation != OperationRecover {
		return false
	}
	switch receipt.Result {
	case ResultRemoved:
		return receipt.Operation == OperationRetire && receipt.Decision == DecisionReady && receipt.Recovery == RecoveryTrackedCommit
	case ResultBlockedChanged:
		return receipt.Decision == DecisionBlocked && (receipt.Recovery == RecoveryTrackedCommit || receipt.Recovery == "none")
	case ResultNativeActionFailed, ResultActionUnverified, ResultRecoveryFailed:
		return receipt.Decision == DecisionBlocked && (receipt.Recovery == RecoveryTrackedCommit || receipt.Recovery == "none")
	case ResultRecovered:
		return receipt.Operation == OperationRecover && receipt.Decision == DecisionReady && receipt.Recovery == RecoveryTrackedCommit
	default:
		return false
	}
}

func validCommitOptional(value string) bool {
	return value == "" || validCommit(value)
}

func (e *Engine) History() ([]Receipt, error) {
	if err := e.ensureState(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(e.retireDir(), "receipts"))
	if err != nil {
		return nil, fail(CodeState, "receipt state unavailable")
	}
	result := make([]Receipt, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			return nil, fail(CodeState, "receipt state is unsafe")
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		private, err := e.loadReceipt(id)
		if err != nil {
			return nil, err
		}
		result = append(result, private.Receipt)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Timestamp == result[j].Timestamp {
			return result[i].ReceiptID < result[j].ReceiptID
		}
		return result[i].Timestamp < result[j].Timestamp
	})
	return result, nil
}
