package retire

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion           = "stillmac.retire.v1"
	RuleSetVersion          = "worktree-retirement.v1"
	ActionNativeRemove      = "native-git-worktree-remove"
	RecoveryTrackedCommit   = "tracked-commit-only"
	OwnerReleaseAttestation = "owner-confirmed-no-active-session"
	OwnerManagedAttestation = "owner-confirmed-user-managed-worktree"
	ReleaseTTL              = 30 * time.Minute
	PlanTTL                 = 15 * time.Minute
)

const (
	DecisionBlocked   = "BLOCKED"
	DecisionReview    = "REVIEW"
	DecisionReady     = "READY"
	DecisionProtected = "PROTECTED"
	DecisionRetired   = "RETIRED"
)

const (
	ReasonMainWorktree          = "main worktree"
	ReasonCurrentWorktree       = "current worktree"
	ReasonLocked                = "worktree is locked"
	ReasonPrunable              = "worktree is prunable"
	ReasonNotLinked             = "target is not a linked worktree"
	ReasonAgentRoot             = "target is under an agent root"
	ReasonUnsafePath            = "worktree path or parent is unsafe"
	ReasonSymlinkOrSpecial      = "worktree contains a symlink or special file"
	ReasonTrackedChanges        = "worktree has tracked or staged changes"
	ReasonUntrackedChanges      = "worktree contains untracked material"
	ReasonIgnoredChanges        = "worktree contains ignored material"
	ReasonSubmodule             = "worktree contains a submodule"
	ReasonGitUnavailable        = "Git state is unavailable"
	ReasonCommitNotPreserved    = "HEAD commit has no preserved local reference"
	ReasonPreservationChanged   = "preserved commit reference changed"
	ReasonUnsupportedGitConfig  = "unsupported Git execution configuration"
	ReasonOwnerReleaseRequired  = "owner release attestation required"
	ReasonOwnerReleaseExpired   = "owner release attestation expired"
	ReasonProtected             = "registration is protected"
	ReasonPlanChanged           = "registered worktree changed after planning"
	ReasonNativeActionFailed    = "native Git action failed; inspect state before retrying"
	ReasonActionUnverified      = "native Git result could not be verified"
	ReasonRecoveryLimit         = "recovery reconstructs tracked files only; ignored and untracked files cannot be restored"
	ReasonCommitUnavailable     = "retained commit is unavailable"
	ReasonRecoveryAlreadyExists = "exact recovery root is occupied or already registered"
	ReasonRegistrationChanged   = "registration changed after planning"
)

const (
	StatusRegistered = "registered"
	StatusReleased   = "released"
	StatusRetired    = "retired"
	StatusBlocked    = "blocked"
)

const (
	ResultRemoved            = "removed"
	ResultBlockedChanged     = "blocked_changed"
	ResultNativeActionFailed = "native_action_failed"
	ResultActionUnverified   = "action_unverified"
	ResultRecovered          = "reconstructed_tracked_checkout"
	ResultRecoveryFailed     = "recovery_failed"
)

const (
	OperationRetire  = "retire"
	OperationRecover = "recover"
)

type ErrorCode string

const (
	CodeUnknown        ErrorCode = "unknown"
	CodeInvalidRequest ErrorCode = "invalid_request"
	CodeState          ErrorCode = "state_error"
	CodeUnavailable    ErrorCode = "unavailable"
	CodeNotFound       ErrorCode = "not_found"
	CodeBlocked        ErrorCode = "blocked"
	CodeChanged        ErrorCode = "changed"
	CodeExpired        ErrorCode = "expired"
	CodeTampered       ErrorCode = "tampered"
	CodeActionFailed   ErrorCode = "action_failed"
	CodeAlreadyRetired ErrorCode = "already_retired"
	CodeRecoveryFailed ErrorCode = "recovery_failed"
)

type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Message }

func CodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return CodeUnknown
}

func fail(code ErrorCode, message string) error {
	return &Error{Code: code, Message: message}
}

type Config struct {
	Home        string
	DataDir     string
	HostID      string
	CurrentPath string
	Now         func() time.Time
	GitRunner   GitRunner
}

type Engine struct{ Config Config }

func New(c Config) *Engine { return &Engine{Config: c} }

type RegisterRequest struct {
	Path                 string
	OwnershipAttestation string
}

type ReleaseRequest struct {
	RegistrationID string
	Attestation    string
}

type PlanRequest struct {
	RegistrationID string
}

type Registration struct {
	SchemaVersion string   `json:"schema_version"`
	ID            string   `json:"registration_id"`
	Status        string   `json:"status"`
	Decision      string   `json:"decision"`
	Reasons       []string `json:"reasons"`
	CapturedAt    string   `json:"captured_at"`
	Label         string   `json:"label"`
	HeadCommit    string   `json:"head_commit"`
	Action        string   `json:"action"`
	Recovery      string   `json:"recovery"`
	Protected     bool     `json:"protected"`
}

type Plan struct {
	SchemaVersion      string       `json:"schema_version"`
	PlanID             string       `json:"plan_id"`
	PlanHash           string       `json:"plan_hash"`
	RegistrationID     string       `json:"registration_id"`
	ExpiresAt          string       `json:"expires_at"`
	HostBinding        string       `json:"host_binding"`
	RuleSet            string       `json:"rule_set"`
	TargetRegistryHash string       `json:"target_registry_hash"`
	Registration       Registration `json:"registration"`
	ApprovalRequired   bool         `json:"approval_required"`
}

type Approval struct {
	SchemaVersion string `json:"schema_version"`
	PlanID        string `json:"plan_id"`
	PlanHash      string `json:"plan_hash"`
	ExpiresAt     string `json:"expires_at"`
	ApprovedAt    string `json:"approved_at"`
}

type Receipt struct {
	SchemaVersion  string   `json:"schema_version"`
	ReceiptID      string   `json:"receipt_id"`
	Operation      string   `json:"operation"`
	RegistrationID string   `json:"registration_id"`
	PlanID         string   `json:"plan_id"`
	PlanHash       string   `json:"plan_hash"`
	Decision       string   `json:"decision"`
	Result         string   `json:"result"`
	Reasons        []string `json:"reasons"`
	RetainedCommit string   `json:"retained_commit"`
	Recovery       string   `json:"recovery"`
	Timestamp      string   `json:"timestamp"`
}

type ApplyResult struct {
	SchemaVersion string  `json:"schema_version"`
	PlanID        string  `json:"plan_id"`
	PlanHash      string  `json:"plan_hash"`
	Receipt       Receipt `json:"receipt"`
}

type RecoveryResult struct {
	SchemaVersion            string `json:"schema_version"`
	ReceiptID                string `json:"receipt_id"`
	Result                   string `json:"result"`
	RetainedCommit           string `json:"retained_commit"`
	RestoredIgnoredUntracked bool   `json:"restored_ignored_untracked"`
	Message                  string `json:"message"`
	Timestamp                string `json:"timestamp"`
}

type GitResult struct {
	Output   []byte
	ExitCode int
}

type GitRunner func(dir string, args []string, env []string) (GitResult, error)

type fsIdentity struct {
	Device    uint64 `json:"device"`
	Inode     uint64 `json:"inode"`
	Size      int64  `json:"size"`
	ModTimeNS int64  `json:"mod_time_ns"`
	Mode      uint32 `json:"mode"`
	Owner     uint32 `json:"owner"`
}

type machineSnapshot struct {
	TargetPath      string     `json:"target_path"`
	MainRoot        string     `json:"main_root"`
	HeadCommit      string     `json:"head_commit"`
	BranchRef       string     `json:"branch_ref"`
	PreserveRef     string     `json:"preserve_ref"`
	PreserveOID     string     `json:"preserve_oid"`
	TargetIdentity  fsIdentity `json:"target_identity"`
	IndexIdentity   fsIdentity `json:"index_identity"`
	TreeFingerprint string     `json:"tree_fingerprint"`
	WorktreeDigest  string     `json:"worktree_digest"`
}

type privateRegistration struct {
	SchemaVersion    string     `json:"schema_version"`
	RegistrationID   string     `json:"registration_id"`
	TargetPath       string     `json:"target_path"`
	MainRoot         string     `json:"main_root"`
	HeadCommit       string     `json:"head_commit"`
	BranchRef        string     `json:"branch_ref"`
	PreserveRef      string     `json:"preserve_ref"`
	PreserveOID      string     `json:"preserve_oid"`
	TargetIdentity   fsIdentity `json:"target_identity"`
	IndexIdentity    fsIdentity `json:"index_identity"`
	TreeFingerprint  string     `json:"tree_fingerprint"`
	WorktreeDigest   string     `json:"worktree_digest"`
	RegisteredAt     string     `json:"registered_at"`
	ReleasedAt       string     `json:"released_at"`
	ReleaseExpiresAt string     `json:"release_expires_at"`
	Attestation      string     `json:"attestation"`
	RetiredAt        string     `json:"retired_at"`
	RecoveredAt      string     `json:"recovered_at"`
	LastReceiptID    string     `json:"last_receipt_id"`
	Hash             string     `json:"registration_hash"`
}

type privateTarget struct {
	SchemaVersion    string              `json:"schema_version"`
	PlanID           string              `json:"plan_id"`
	RegistrationID   string              `json:"registration_id"`
	RegistrationHash string              `json:"registration_hash"`
	HostID           string              `json:"host_id"`
	Registration     privateRegistration `json:"registration"`
	Snapshot         machineSnapshot     `json:"snapshot"`
	Hash             string              `json:"target_hash"`
}

type privateApproval struct {
	SchemaVersion      string `json:"schema_version"`
	PlanID             string `json:"plan_id"`
	PlanHash           string `json:"plan_hash"`
	TargetRegistryHash string `json:"target_registry_hash"`
	HostID             string `json:"host_id"`
	ExpiresAt          string `json:"expires_at"`
	ApprovedAt         string `json:"approved_at"`
	Hash               string `json:"approval_hash"`
}

type privateProtection struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"registration_id"`
	Protected     bool   `json:"protected"`
	Hash          string `json:"protection_hash"`
}

type privateReceipt struct {
	SchemaVersion string  `json:"schema_version"`
	Receipt       Receipt `json:"receipt"`
	TargetPath    string  `json:"target_path"`
	MainRoot      string  `json:"main_root"`
	HeadCommit    string  `json:"head_commit"`
	PreserveRef   string  `json:"preserve_ref"`
	PreserveOID   string  `json:"preserve_oid"`
	RecoveredAt   string  `json:"recovered_at"`
	Hash          string  `json:"receipt_hash"`
}

type hostRecord struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
}

type worktreeRecord struct {
	Path     string
	Head     string
	Branch   string
	Locked   bool
	Prunable bool
}

func (e *Engine) now() time.Time {
	if e.Config.Now != nil {
		return e.Config.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) home() (string, error) {
	if e.Config.Home != "" {
		return absoluteCleanPath(e.Config.Home)
	}
	return os.UserHomeDir()
}

func (e *Engine) currentPath() (string, error) {
	var path string
	if e.Config.CurrentPath != "" {
		path = e.Config.CurrentPath
	} else {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	path, err := absoluteCleanPath(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return absoluteCleanPath(resolved)
}

func absoluteCleanPath(path string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 || !filepath.IsAbs(path) {
		return "", fail(CodeInvalidRequest, "absolute path required")
	}
	clean := filepath.Clean(path)
	if clean != path {
		return "", fail(CodeInvalidRequest, "clean absolute path required")
	}
	return clean, nil
}

func validID(id, prefix string) bool {
	if !strings.HasPrefix(id, prefix) || len(id) <= len(prefix) {
		return false
	}
	wantLength := 0
	switch prefix {
	case "retire-":
		wantLength = 32
	case "retire-plan-":
		wantLength = 16
	case "retire-receipt-", "retire-recover-":
		wantLength = 24
	}
	if wantLength != 0 && len(id[len(prefix):]) != wantLength {
		return false
	}
	for _, r := range id[len(prefix):] {
		if (r < 'a' || r > 'f') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validCommit(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func opaqueHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func newID(prefix string) (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", fail(CodeState, "private state identity unavailable")
	}
	return prefix + hex.EncodeToString(random), nil
}

func samePathOrDescendant(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func addReason(reasons *[]string, reason string) {
	for _, existing := range *reasons {
		if existing == reason {
			return
		}
	}
	*reasons = append(*reasons, reason)
}

func sortReasons(reasons []string) []string {
	result := append([]string(nil), reasons...)
	sort.Strings(result)
	return result
}
