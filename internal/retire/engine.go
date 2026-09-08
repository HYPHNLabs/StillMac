package retire

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

func (e *Engine) Register(request RegisterRequest) (Registration, error) {
	if request.OwnershipAttestation != OwnerManagedAttestation {
		return Registration{}, fail(CodeInvalidRequest, "user-managed ownership attestation is required")
	}
	if err := e.ensureState(); err != nil {
		return Registration{}, err
	}
	path, err := absoluteCleanPath(request.Path)
	if err != nil {
		return Registration{}, err
	}
	id := registrationID(path)
	protected, err := e.protection(id)
	if err != nil {
		return Registration{}, err
	}
	reg := privateRegistration{
		SchemaVersion:  SchemaVersion,
		RegistrationID: id,
		TargetPath:     path,
		RegisteredAt:   e.now().Format(time.RFC3339),
	}
	var snapshot machineSnapshot
	var reasons []string
	home, homeErr := e.home()
	if homeErr != nil {
		addReason(&reasons, ReasonGitUnavailable)
	} else if isAgentRoot(home, path) {
		addReason(&reasons, ReasonAgentRoot)
	} else if info, statErr := os.Lstat(path); errors.Is(statErr, os.ErrNotExist) {
		return Registration{}, fail(CodeNotFound, "worktree target was not found")
	} else if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || validateExistingDirectoryPath(path) != nil {
		addReason(&reasons, ReasonUnsafePath)
	} else {
		snapshot, reasons, _ = e.inspectPath(home, path, path)
	}
	applySnapshot(&reg, snapshot)
	if err := e.writeRegistration(reg); err != nil {
		return Registration{}, fail(CodeState, "registration state could not be written")
	}
	return publicRegistration(reg, snapshot, reasons, protected, e.now()), nil
}

func (e *Engine) Release(request ReleaseRequest) (Registration, error) {
	if request.Attestation != OwnerReleaseAttestation {
		return Registration{}, fail(CodeInvalidRequest, "owner release attestation is required")
	}
	if err := e.ensureState(); err != nil {
		return Registration{}, err
	}
	reg, err := e.loadRegistration(request.RegistrationID)
	if err != nil {
		return Registration{}, err
	}
	if reg.RetiredAt != "" {
		return publicRegistration(reg, machineSnapshot{}, nil, false, e.now()), fail(CodeAlreadyRetired, "worktree is already retired")
	}
	protected, err := e.protection(reg.RegistrationID)
	if err != nil {
		return Registration{}, err
	}
	snapshot, reasons, _ := e.inspectRegistration(reg)
	if protected {
		return publicRegistration(reg, snapshot, nil, true, e.now()), fail(CodeBlocked, "registration is protected")
	}
	if len(reasons) != 0 {
		return publicRegistration(reg, snapshot, reasons, false, e.now()), fail(CodeBlocked, "machine checks block release")
	}
	now := e.now()
	applySnapshot(&reg, snapshot)
	reg.ReleasedAt = now.Format(time.RFC3339)
	reg.ReleaseExpiresAt = now.Add(ReleaseTTL).Format(time.RFC3339)
	reg.Attestation = OwnerReleaseAttestation
	if err := e.writeRegistration(reg); err != nil {
		return Registration{}, fail(CodeState, "release attestation could not be written")
	}
	return publicRegistration(reg, snapshot, nil, false, now), nil
}

func (e *Engine) List() ([]Registration, error) {
	if err := e.ensureState(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(e.retireDir(), "registrations"))
	if err != nil {
		return nil, fail(CodeState, "registration state unavailable")
	}
	result := make([]Registration, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			return nil, fail(CodeState, "registration state is unsafe")
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		reg, err := e.loadRegistration(id)
		if err != nil {
			return nil, err
		}
		protected, err := e.protection(id)
		if err != nil {
			return nil, err
		}
		if reg.RetiredAt != "" {
			result = append(result, publicRegistration(reg, machineSnapshot{}, nil, protected, e.now()))
			continue
		}
		snapshot, reasons, _ := e.inspectRegistration(reg)
		result = append(result, publicRegistration(reg, snapshot, reasons, protected, e.now()))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (e *Engine) Protect(registrationID string) error {
	if err := e.ensureState(); err != nil {
		return err
	}
	if _, err := e.loadRegistration(registrationID); err != nil {
		return err
	}
	if err := e.writeProtection(registrationID, true); err != nil {
		return fail(CodeState, "protection state could not be written")
	}
	return nil
}

func (e *Engine) Unprotect(registrationID string) error {
	if err := e.ensureState(); err != nil {
		return err
	}
	if _, err := e.loadRegistration(registrationID); err != nil {
		return err
	}
	if err := e.writeProtection(registrationID, false); err != nil {
		return fail(CodeState, "protection state could not be written")
	}
	return nil
}

func (e *Engine) Plan(request PlanRequest) (Plan, error) {
	if err := e.ensureState(); err != nil {
		return Plan{}, err
	}
	reg, err := e.loadRegistration(request.RegistrationID)
	if err != nil {
		return Plan{}, err
	}
	protected, err := e.protection(reg.RegistrationID)
	if err != nil {
		return Plan{}, err
	}
	snapshot := machineSnapshot{}
	var reasons []string
	if reg.RetiredAt == "" {
		snapshot, reasons, _ = e.inspectRegistration(reg)
	}
	hostID, err := e.hostID()
	if err != nil {
		return Plan{}, err
	}
	public := publicRegistration(reg, snapshot, reasons, protected, e.now())
	target := privateTarget{
		SchemaVersion:    SchemaVersion,
		RegistrationID:   reg.RegistrationID,
		RegistrationHash: reg.Hash,
		HostID:           hostID,
		Registration:     reg,
		Snapshot:         snapshot,
	}
	target.Hash = targetHash(target)
	plan := Plan{
		SchemaVersion:      SchemaVersion,
		RegistrationID:     reg.RegistrationID,
		ExpiresAt:          e.now().Add(PlanTTL).Format(time.RFC3339),
		HostBinding:        opaqueHash(hostID),
		RuleSet:            RuleSetVersion,
		TargetRegistryHash: target.Hash,
		Registration:       public,
		ApprovalRequired:   true,
	}
	plan.PlanHash = planHash(plan)
	plan.PlanID = "retire-plan-" + plan.PlanHash[:16]
	target.PlanID = plan.PlanID
	if err := atomicJSON(e.statePath("targets", plan.PlanID), target); err != nil {
		return Plan{}, fail(CodeState, "target registry could not be written")
	}
	if err := atomicJSON(e.statePath("plans", plan.PlanID), plan); err != nil {
		return Plan{}, fail(CodeState, "plan state could not be written")
	}
	return plan, nil
}

func (e *Engine) Approve(planID string) (Approval, error) {
	if err := e.ensureState(); err != nil {
		return Approval{}, err
	}
	plan, target, err := e.loadPlan(planID)
	if err != nil {
		return Approval{}, err
	}
	if err := ensureUnexpired(plan.ExpiresAt, e.now()); err != nil {
		return Approval{}, err
	}
	if plan.Registration.Decision != DecisionReady {
		return Approval{}, fail(CodeBlocked, "blocked plan cannot be approved")
	}
	hostID, err := e.hostID()
	if err != nil {
		return Approval{}, err
	}
	if plan.HostBinding != opaqueHash(hostID) || target.HostID != hostID {
		return Approval{}, fail(CodeTampered, "plan host binding is invalid")
	}
	now := e.now()
	approval := privateApproval{
		SchemaVersion:      SchemaVersion,
		PlanID:             plan.PlanID,
		PlanHash:           plan.PlanHash,
		TargetRegistryHash: plan.TargetRegistryHash,
		HostID:             hostID,
		ExpiresAt:          plan.ExpiresAt,
		ApprovedAt:         now.Format(time.RFC3339),
	}
	approval.Hash = approvalHash(approval)
	if err := atomicJSON(e.statePath("approvals", plan.PlanID), approval); err != nil {
		return Approval{}, fail(CodeState, "plan approval could not be written")
	}
	return Approval{SchemaVersion: SchemaVersion, PlanID: approval.PlanID, PlanHash: approval.PlanHash, ExpiresAt: approval.ExpiresAt, ApprovedAt: approval.ApprovedAt}, nil
}

func (e *Engine) Apply(planID string) (ApplyResult, error) {
	result := ApplyResult{SchemaVersion: SchemaVersion, PlanID: planID}
	if err := e.ensureState(); err != nil {
		return result, err
	}
	plan, target, err := e.loadPlan(planID)
	if err != nil {
		return result, err
	}
	result.PlanHash = plan.PlanHash
	if err := ensureUnexpired(plan.ExpiresAt, e.now()); err != nil {
		return result, err
	}
	approval, err := e.loadApproval(planID)
	if err != nil {
		return result, err
	}
	if approval.PlanHash != plan.PlanHash || approval.TargetRegistryHash != plan.TargetRegistryHash || approval.ExpiresAt != plan.ExpiresAt {
		return result, fail(CodeTampered, "plan approval does not match plan")
	}
	if err := ensureUnexpired(approval.ExpiresAt, e.now()); err != nil {
		return result, err
	}
	hostID, err := e.hostID()
	if err != nil {
		return result, err
	}
	if approval.HostID != hostID || plan.HostBinding != opaqueHash(hostID) || target.HostID != hostID {
		return result, fail(CodeTampered, "plan host binding is invalid")
	}
	if plan.Registration.Decision != DecisionReady {
		return e.blockedApplyResult(result, plan, target, []string{ReasonPlanChanged}, CodeBlocked)
	}
	reg, err := e.loadRegistration(plan.RegistrationID)
	if err != nil {
		return result, err
	}
	if reg.Hash != target.RegistrationHash {
		return e.blockedApplyResult(result, plan, target, []string{ReasonRegistrationChanged}, CodeChanged)
	}
	protected, err := e.protection(reg.RegistrationID)
	if err != nil {
		return result, err
	}
	if protected {
		return e.blockedApplyResult(result, plan, target, []string{ReasonProtected}, CodeBlocked)
	}
	snapshot, reasons, _ := e.inspectRegistration(reg)
	if !releaseValid(reg, e.now()) {
		addReason(&reasons, ReasonOwnerReleaseExpired)
	}
	if len(reasons) != 0 || !reflect.DeepEqual(snapshot, target.Snapshot) {
		if len(reasons) == 0 {
			reasons = []string{ReasonPlanChanged}
		}
		return e.blockedApplyResult(result, plan, target, reasons, CodeChanged)
	}
	if snapshot.MainRoot == "" || validateExistingDirectoryPath(snapshot.MainRoot) != nil {
		return e.blockedApplyResult(result, plan, target, []string{ReasonUnsafePath}, CodeChanged)
	}
	receiptID, err := newID("retire-receipt-")
	if err != nil {
		return result, err
	}
	commandResult, commandErr := e.runGit(snapshot.MainRoot, withSafeGitOptions("worktree", "remove", snapshot.TargetPath)...)
	if commandErr != nil || commandResult.ExitCode != 0 {
		receipt := Receipt{SchemaVersion: SchemaVersion, ReceiptID: receiptID, Operation: OperationRetire, RegistrationID: reg.RegistrationID, PlanID: plan.PlanID, PlanHash: plan.PlanHash, Decision: DecisionBlocked, Result: ResultNativeActionFailed, Reasons: []string{ReasonNativeActionFailed}, RetainedCommit: snapshot.HeadCommit, Recovery: recoveryFor(snapshot), Timestamp: e.now().Format(time.RFC3339)}
		if writeErr := e.writeReceipt(privateReceipt{SchemaVersion: SchemaVersion, Receipt: receipt, TargetPath: snapshot.TargetPath, MainRoot: snapshot.MainRoot, HeadCommit: snapshot.HeadCommit, PreserveRef: snapshot.PreserveRef, PreserveOID: snapshot.PreserveOID}); writeErr != nil {
			return result, fail(CodeState, "action failed and receipt could not be written")
		}
		result.Receipt = receipt
		return result, fail(CodeActionFailed, "native Git action failed")
	}
	if !e.removalVerified(snapshot.MainRoot, snapshot.TargetPath) {
		receipt := Receipt{SchemaVersion: SchemaVersion, ReceiptID: receiptID, Operation: OperationRetire, RegistrationID: reg.RegistrationID, PlanID: plan.PlanID, PlanHash: plan.PlanHash, Decision: DecisionBlocked, Result: ResultActionUnverified, Reasons: []string{ReasonActionUnverified}, RetainedCommit: snapshot.HeadCommit, Recovery: recoveryFor(snapshot), Timestamp: e.now().Format(time.RFC3339)}
		if writeErr := e.writeReceipt(privateReceipt{SchemaVersion: SchemaVersion, Receipt: receipt, TargetPath: snapshot.TargetPath, MainRoot: snapshot.MainRoot, HeadCommit: snapshot.HeadCommit, PreserveRef: snapshot.PreserveRef, PreserveOID: snapshot.PreserveOID}); writeErr != nil {
			return result, fail(CodeState, "action result was unverified and receipt could not be written")
		}
		result.Receipt = receipt
		return result, fail(CodeActionFailed, "native Git result could not be verified")
	}
	receipt := Receipt{SchemaVersion: SchemaVersion, ReceiptID: receiptID, Operation: OperationRetire, RegistrationID: reg.RegistrationID, PlanID: plan.PlanID, PlanHash: plan.PlanHash, Decision: DecisionReady, Result: ResultRemoved, RetainedCommit: snapshot.HeadCommit, Recovery: recoveryFor(snapshot), Timestamp: e.now().Format(time.RFC3339)}
	private := privateReceipt{SchemaVersion: SchemaVersion, Receipt: receipt, TargetPath: snapshot.TargetPath, MainRoot: snapshot.MainRoot, HeadCommit: snapshot.HeadCommit, PreserveRef: snapshot.PreserveRef, PreserveOID: snapshot.PreserveOID}
	if err := e.writeReceipt(private); err != nil {
		return result, fail(CodeState, "removal succeeded but receipt could not be written")
	}
	result.Receipt = receipt
	reg.RetiredAt = e.now().Format(time.RFC3339)
	reg.LastReceiptID = receiptID
	if err := e.writeRegistration(reg); err != nil {
		return result, fail(CodeState, "removal succeeded but registration state could not be updated")
	}
	return result, nil
}

func (e *Engine) blockedApplyResult(result ApplyResult, plan Plan, target privateTarget, reasons []string, code ErrorCode) (ApplyResult, error) {
	receiptID, err := newID("retire-receipt-")
	if err != nil {
		return result, err
	}
	reasons = sortReasons(reasons)
	receipt := Receipt{SchemaVersion: SchemaVersion, ReceiptID: receiptID, Operation: OperationRetire, RegistrationID: plan.RegistrationID, PlanID: plan.PlanID, PlanHash: plan.PlanHash, Decision: DecisionBlocked, Result: ResultBlockedChanged, Reasons: reasons, RetainedCommit: target.Snapshot.HeadCommit, Recovery: recoveryFor(target.Snapshot), Timestamp: e.now().Format(time.RFC3339)}
	private := privateReceipt{SchemaVersion: SchemaVersion, Receipt: receipt, TargetPath: target.Snapshot.TargetPath, MainRoot: target.Snapshot.MainRoot, HeadCommit: target.Snapshot.HeadCommit, PreserveRef: target.Snapshot.PreserveRef, PreserveOID: target.Snapshot.PreserveOID}
	if writeErr := e.writeReceipt(private); writeErr != nil {
		return result, fail(CodeState, "blocked result receipt could not be written")
	}
	result.Receipt = receipt
	return result, fail(code, "plan no longer permits the requested action")
}

func (e *Engine) removalVerified(main, target string) bool {
	list, err := e.worktreeList(main)
	if err != nil {
		return false
	}
	for _, record := range list {
		if record.Path == target {
			return false
		}
	}
	_, err = os.Lstat(target)
	return errors.Is(err, os.ErrNotExist)
}

func (e *Engine) Recover(receiptID string) (RecoveryResult, error) {
	result := RecoveryResult{SchemaVersion: SchemaVersion, ReceiptID: receiptID, RestoredIgnoredUntracked: false}
	if err := e.ensureState(); err != nil {
		return result, err
	}
	private, err := e.loadReceipt(receiptID)
	if err != nil {
		return result, err
	}
	if private.Receipt.Operation != OperationRetire || private.Receipt.Result != ResultRemoved {
		return result, fail(CodeBlocked, "receipt does not support recovery")
	}
	if !validCommit(private.HeadCommit) || private.PreserveRef == "" || !validCommit(private.PreserveOID) {
		return result, fail(CodeBlocked, ReasonCommitUnavailable)
	}
	home, err := e.home()
	if err != nil {
		return result, err
	}
	if isAgentRoot(home, private.TargetPath) || validateParentsForCreation(private.TargetPath) != nil || validateExistingDirectoryPath(private.MainRoot) != nil {
		return e.recoveryFailure(result, private, ReasonUnsafePath)
	}
	if _, err := os.Lstat(private.TargetPath); err == nil {
		return e.recoveryFailure(result, private, ReasonRecoveryAlreadyExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return e.recoveryFailure(result, private, ReasonRecoveryAlreadyExists)
	}
	list, err := e.worktreeList(private.MainRoot)
	if err != nil {
		return e.recoveryFailure(result, private, ReasonGitUnavailable)
	}
	for _, record := range list {
		if record.Path == private.TargetPath {
			return e.recoveryFailure(result, private, ReasonRecoveryAlreadyExists)
		}
	}
	if err := e.checkGitExecutionConfig(private.MainRoot); err != nil {
		return e.recoveryFailure(result, private, ReasonUnsupportedGitConfig)
	}
	preserved, err := e.gitOutput(private.MainRoot, "rev-parse", "--verify", private.PreserveRef+"^{commit}")
	if err != nil || strings.TrimSpace(string(preserved)) != private.PreserveOID {
		return e.recoveryFailure(result, private, ReasonPreservationChanged)
	}
	commitResult, commitErr := e.runGit(private.MainRoot, withSafeGitOptions("cat-file", "-e", private.HeadCommit+"^{commit}")...)
	if commitErr != nil || commitResult.ExitCode != 0 {
		return e.recoveryFailure(result, private, ReasonCommitUnavailable)
	}
	tree, treeErr := e.gitOutput(private.MainRoot, "ls-tree", "-r", "-z", "--full-tree", private.HeadCommit)
	if treeErr != nil {
		return e.recoveryFailure(result, private, ReasonCommitUnavailable)
	}
	if hasSubmoduleMode(tree) {
		return e.recoveryFailure(result, private, ReasonSubmodule)
	}
	commandResult, commandErr := e.runGit(private.MainRoot, withSafeGitOptions("worktree", "add", "--detach", private.TargetPath, private.HeadCommit)...)
	if commandErr != nil || commandResult.ExitCode != 0 {
		return e.recoveryFailure(result, private, ReasonNativeActionFailed)
	}
	if !e.recoveryVerified(private.MainRoot, private.TargetPath, private.HeadCommit) {
		return e.recoveryFailure(result, private, ReasonActionUnverified)
	}
	recoveryID, err := newID("retire-recover-")
	if err != nil {
		return result, err
	}
	now := e.now()
	receipt := Receipt{SchemaVersion: SchemaVersion, ReceiptID: recoveryID, Operation: OperationRecover, RegistrationID: private.Receipt.RegistrationID, PlanID: private.Receipt.PlanID, PlanHash: private.Receipt.PlanHash, Decision: DecisionReady, Result: ResultRecovered, RetainedCommit: private.HeadCommit, Recovery: RecoveryTrackedCommit, Timestamp: now.Format(time.RFC3339)}
	if err := e.writeReceipt(privateReceipt{SchemaVersion: SchemaVersion, Receipt: receipt, TargetPath: private.TargetPath, MainRoot: private.MainRoot, HeadCommit: private.HeadCommit, PreserveRef: private.PreserveRef, PreserveOID: private.PreserveOID}); err != nil {
		return result, fail(CodeState, "recovery succeeded but receipt could not be written")
	}
	result.ReceiptID = recoveryID
	result.Result = ResultRecovered
	result.RetainedCommit = private.HeadCommit
	result.Message = ReasonRecoveryLimit
	result.Timestamp = now.Format(time.RFC3339)
	registration, registrationErr := e.loadRegistration(private.Receipt.RegistrationID)
	if registrationErr != nil {
		return result, fail(CodeState, "recovery succeeded but registration state could not be read")
	}
	registration.RetiredAt = ""
	registration.RecoveredAt = now.Format(time.RFC3339)
	registration.ReleasedAt = ""
	registration.ReleaseExpiresAt = ""
	registration.Attestation = ""
	registration.LastReceiptID = recoveryID
	if snapshot, reasons, _ := e.inspectRegistration(registration); len(reasons) == 0 {
		applySnapshot(&registration, snapshot)
	}
	if err := e.writeRegistration(registration); err != nil {
		return result, fail(CodeState, "recovery succeeded but registration state could not be updated")
	}
	return result, nil
}

func (e *Engine) recoveryVerified(main, target, head string) bool {
	list, err := e.worktreeList(main)
	if err != nil {
		return false
	}
	for _, record := range list {
		if record.Path == target {
			return record.Head == head && !record.Locked && !record.Prunable
		}
	}
	return false
}

func (e *Engine) recoveryFailure(result RecoveryResult, original privateReceipt, reason string) (RecoveryResult, error) {
	recoveryID, err := newID("retire-recover-")
	if err != nil {
		return result, err
	}
	receipt := Receipt{SchemaVersion: SchemaVersion, ReceiptID: recoveryID, Operation: OperationRecover, RegistrationID: original.Receipt.RegistrationID, PlanID: original.Receipt.PlanID, PlanHash: original.Receipt.PlanHash, Decision: DecisionBlocked, Result: ResultRecoveryFailed, Reasons: []string{reason}, RetainedCommit: original.HeadCommit, Recovery: recoveryForPrivate(original), Timestamp: e.now().Format(time.RFC3339)}
	private := privateReceipt{SchemaVersion: SchemaVersion, Receipt: receipt, TargetPath: original.TargetPath, MainRoot: original.MainRoot, HeadCommit: original.HeadCommit, PreserveRef: original.PreserveRef, PreserveOID: original.PreserveOID}
	if writeErr := e.writeReceipt(private); writeErr != nil {
		return result, fail(CodeState, "recovery failure receipt could not be written")
	}
	result.ReceiptID = recoveryID
	result.Result = ResultRecoveryFailed
	result.RetainedCommit = original.HeadCommit
	result.Message = reason
	result.Timestamp = e.now().Format(time.RFC3339)
	return result, fail(CodeRecoveryFailed, reason)
}

func applySnapshot(reg *privateRegistration, snapshot machineSnapshot) {
	if snapshot.TargetPath == "" {
		return
	}
	reg.TargetPath = snapshot.TargetPath
	reg.MainRoot = snapshot.MainRoot
	reg.HeadCommit = snapshot.HeadCommit
	reg.BranchRef = snapshot.BranchRef
	reg.PreserveRef = snapshot.PreserveRef
	reg.PreserveOID = snapshot.PreserveOID
	reg.TargetIdentity = snapshot.TargetIdentity
	reg.IndexIdentity = snapshot.IndexIdentity
	reg.TreeFingerprint = snapshot.TreeFingerprint
	reg.WorktreeDigest = snapshot.WorktreeDigest
}

func releaseValid(reg privateRegistration, now time.Time) bool {
	if reg.Attestation != OwnerReleaseAttestation || reg.ReleasedAt == "" || reg.ReleaseExpiresAt == "" {
		return false
	}
	expires, err := time.Parse(time.RFC3339, reg.ReleaseExpiresAt)
	return err == nil && now.Before(expires)
}

func publicRegistration(reg privateRegistration, snapshot machineSnapshot, reasons []string, protected bool, now time.Time) Registration {
	head := reg.HeadCommit
	if snapshot.HeadCommit != "" {
		head = snapshot.HeadCommit
	}
	preserveRef := reg.PreserveRef
	preserveOID := reg.PreserveOID
	if snapshot.PreserveRef != "" {
		preserveRef = snapshot.PreserveRef
		preserveOID = snapshot.PreserveOID
	}
	public := Registration{
		SchemaVersion: SchemaVersion,
		ID:            reg.RegistrationID,
		Status:        StatusRegistered,
		Decision:      DecisionReview,
		Reasons:       sortReasons(reasons),
		CapturedAt:    now.Format(time.RFC3339),
		Label:         "Git linked worktree",
		HeadCommit:    head,
		Recovery:      "none",
	}
	if validCommit(head) && preserveRef != "" && validCommit(preserveOID) {
		public.Recovery = RecoveryTrackedCommit
	}
	if reg.RetiredAt != "" {
		public.Status = StatusRetired
		public.Decision = DecisionRetired
		public.Action = "none"
		return public
	}
	if protected {
		public.Status = StatusBlocked
		public.Decision = DecisionProtected
		public.Protected = true
		public.Reasons = []string{ReasonProtected}
		public.Action = "none"
		return public
	}
	if len(public.Reasons) != 0 {
		public.Status = StatusBlocked
		public.Decision = DecisionBlocked
		public.Action = "none"
		return public
	}
	if !releaseValid(reg, now) {
		public.Status = StatusRegistered
		public.Decision = DecisionReview
		if reg.Attestation == OwnerReleaseAttestation && reg.ReleaseExpiresAt != "" {
			public.Reasons = []string{ReasonOwnerReleaseExpired}
		} else {
			public.Reasons = []string{ReasonOwnerReleaseRequired}
		}
		public.Action = "none"
		return public
	}
	public.Status = StatusReleased
	public.Decision = DecisionReady
	public.Action = ActionNativeRemove
	return public
}

func recoveryFor(snapshot machineSnapshot) string {
	if validCommit(snapshot.HeadCommit) && snapshot.PreserveRef != "" && validCommit(snapshot.PreserveOID) {
		return RecoveryTrackedCommit
	}
	return "none"
}

func recoveryForPrivate(receipt privateReceipt) string {
	if validCommit(receipt.HeadCommit) && receipt.PreserveRef != "" && validCommit(receipt.PreserveOID) {
		return RecoveryTrackedCommit
	}
	return "none"
}

func ensureUnexpired(value string, now time.Time) error {
	expires, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fail(CodeTampered, "expiry is invalid")
	}
	if !now.Before(expires) {
		return fail(CodeExpired, "plan has expired")
	}
	return nil
}
