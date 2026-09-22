package residue

import (
	"errors"
	"math"
	"time"

	"stillmac/internal/cleanup"
)

const (
	SchemaVersion = "stillmac.residue.report.v1"
	RuleSet       = "residue-rules.v1"

	StatusComplete   Status = "complete"
	StatusPartial    Status = "partial"
	StatusUnknown    Status = "unknown"
	StatusNotPresent Status = "not-present"

	SizeComplete   MeasurementStatus = "complete"
	SizePartial    MeasurementStatus = "partial"
	SizeUnknown    MeasurementStatus = "unknown"
	SizeNotPresent MeasurementStatus = "not-present"

	ActionInventoryOnly = "inventory-only"
	ActionOwnerManaged  = "owner-managed"
	ActionUnavailable   = "unavailable"

	ChangeUnchanged = "unchanged"
	ChangeGrowth    = "growth"
	ChangeShrink    = "shrink"
	ChangeReplaced  = "replaced"
	ChangeUnknown   = "unknown"

	defaultHistoryLimit = 32
	maxHistoryLimit     = 256
	maxScopes           = 32
	maxAliasLength      = 128
	limitsApplied       = "explicit-project-artifact-and-worktree-metadata-only; exact-cache-decision-scan-delegated"
)

var (
	ErrInvalidConfig = errors.New("invalid residue configuration")
	ErrInvalidScope  = errors.New("invalid residue scope")
	ErrDataDir       = errors.New("residue data directory unavailable")
	ErrHistory       = errors.New("residue history unavailable")
)

type Status string

type MeasurementStatus string

// Config controls the read-only residue scanner and its private snapshot store.
// Home is used only for StillMac's existing exact cache roots. Project and
// worktree inspection always requires an explicit Scope.
type Config struct {
	Home         string
	DataDir      string
	Now          func() time.Time
	GitRunner    cleanup.GitRunner
	GoCleaner    cleanup.GoCleaner
	Limits       Limits
	Aliases      []Alias
	HistoryLimit int
}

type Limits struct {
	MaxItems    int
	MaxDepth    int
	MaxBytes    int64
	MaxDuration time.Duration
}

type Alias struct {
	ScopeID string
	Label   string
}

type Scope struct {
	Path  string
	Kind  string
	Alias string
}

// GitResult is an alias so CLI adapters can inject the same fixed Git seam as
// the cleanup package without exposing command output in residue reports.
type GitResult = cleanup.GitResult
type GitRunner = cleanup.GitRunner

type Engine struct {
	Config Config

	limits  Limits
	aliases map[string]string
}

type Size struct {
	Status MeasurementStatus `json:"status"`
	Bytes  *int64            `json:"bytes"`
}

type ScopeBinding struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	RootIdentity string `json:"root_identity"`
	Status       Status `json:"status"`
}

type Evidence struct {
	TotalSize         Size     `json:"total_size"`
	LimitsApplied     string   `json:"limits_applied"`
	ItemsVisited      int      `json:"items_visited"`
	ItemsSkipped      int      `json:"items_skipped"`
	HardlinksSkipped  int      `json:"hardlinks_skipped"`
	WorktreesObserved int      `json:"worktrees_observed"`
	ScopesObserved    int      `json:"scopes_observed"`
	Warnings          []string `json:"warnings"`
}

type Action struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type Entry struct {
	ID                 string   `json:"id"`
	ScopeID            string   `json:"scope_id"`
	Kind               string   `json:"kind"`
	Family             string   `json:"family"`
	RuleVersion        string   `json:"rule_version"`
	Size               Size     `json:"size"`
	State              string   `json:"state"`
	Identity           string   `json:"identity"`
	Fingerprint        string   `json:"fingerprint"`
	ActionAvailability string   `json:"action_availability"`
	CleanupCandidateID string   `json:"cleanup_candidate_id,omitempty"`
	CleanupDecision    string   `json:"cleanup_decision,omitempty"`
	CleanupAction      string   `json:"cleanup_action,omitempty"`
	Evidence           []string `json:"evidence"`
	files              map[fileKey]int64
}

type Change struct {
	EntryID       string   `json:"entry_id"`
	ScopeID       string   `json:"scope_id"`
	Family        string   `json:"family"`
	Kind          string   `json:"kind"`
	PreviousBytes *int64   `json:"previous_bytes"`
	CurrentBytes  *int64   `json:"current_bytes"`
	DeltaBytes    *int64   `json:"delta_bytes"`
	Regrowth      bool     `json:"regrowth"`
	Evidence      []string `json:"evidence"`
}

type Report struct {
	SchemaVersion string         `json:"schema_version"`
	ReportID      string         `json:"report_id"`
	SnapshotID    string         `json:"snapshot_id,omitempty"`
	ReportKind    string         `json:"report_kind"`
	Status        Status         `json:"status"`
	CapturedAt    string         `json:"captured_at"`
	RuleSet       string         `json:"rule_set"`
	ScopeBindings []ScopeBinding `json:"scope_bindings"`
	Evidence      Evidence       `json:"evidence"`
	Actions       []Action       `json:"actions"`
	NextStep      string         `json:"next_step"`
	Entries       []Entry        `json:"entries"`
	FromSnapshot  string         `json:"from_snapshot,omitempty"`
	ToSnapshot    string         `json:"to_snapshot,omitempty"`
	Changes       []Change       `json:"changes,omitempty"`
	Quiet         bool           `json:"quiet"`
	Suppressed    bool           `json:"suppressed"`
}

type ChangeOptions struct {
	FromID string
	ToID   string
}

type SessionReportOptions struct {
	Scopes         []Scope
	ThresholdBytes int64
	QuietUnchanged bool
}

func New(config Config) (*Engine, error) {
	limits := config.Limits
	if limits.MaxItems == 0 {
		limits.MaxItems = 4096
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = 16
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = math.MaxInt64
	}
	if limits.MaxDuration == 0 {
		limits.MaxDuration = 3 * time.Second
	}
	if limits.MaxItems < 1 || limits.MaxDepth < 0 || limits.MaxBytes < 1 || limits.MaxDuration < 0 {
		return nil, ErrInvalidConfig
	}
	if config.HistoryLimit == 0 {
		config.HistoryLimit = defaultHistoryLimit
	}
	if config.HistoryLimit < 1 || config.HistoryLimit > maxHistoryLimit {
		return nil, ErrInvalidConfig
	}
	aliases := make(map[string]string, len(config.Aliases))
	for _, alias := range config.Aliases {
		if alias.ScopeID == "" || alias.Label == "" || len([]rune(alias.Label)) > maxAliasLength || hasControl(alias.Label) {
			return nil, ErrInvalidConfig
		}
		if _, exists := aliases[alias.ScopeID]; exists {
			return nil, ErrInvalidConfig
		}
		aliases[alias.ScopeID] = alias.Label
	}
	return &Engine{Config: config, limits: limits, aliases: aliases}, nil
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ScopeID returns the opaque stable binding ID used in machine reports. It
// never returns the supplied path and is safe for a CLI to use when resolving
// an optional human-facing alias.
func ScopeID(scope Scope) (string, error) {
	normalized, err := normalizeScopes([]Scope{scope})
	if err != nil {
		return "", err
	}
	return stableID("scope", normalized[0].Kind+"\x00"+normalized[0].Path), nil
}

// AliasForScope resolves a bounded human-facing label without placing it in
// the machine report. A false result means no alias was configured.
func (e *Engine) AliasForScope(scope Scope) (string, bool) {
	id, err := ScopeID(scope)
	if err != nil {
		return "", false
	}
	label, ok := e.aliases[id]
	return label, ok
}
