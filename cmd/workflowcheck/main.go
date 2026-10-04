package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

type actor struct {
	name string
	role string
}

var (
	actRegistrar  = actor{name: "registrar-du", role: "registrar"}
	actPlanAuthor = actor{name: "lead-wang", role: "prep_lead"}
	actApprover   = actor{name: "lead-han", role: "prep_lead"}
	actPreparator = actor{name: "preparator-chen", role: "preparator"}
	actCurator    = actor{name: "keeper-zhou", role: "curator"}
)

type specimenView struct {
	ID            string `json:"id"`
	CatalogNumber string `json:"catalog_number"`
	Status        string `json:"status"`
	RegisteredBy  string `json:"registered_by"`
}

type planView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	Status     string `json:"status"`
	CreatedBy  string `json:"created_by"`
	ApprovedBy string `json:"approved_by"`
}

type planSnapshot struct {
	Plan     planView `json:"plan"`
	Progress []struct {
		Percent    int    `json:"percent"`
		RecordedBy string `json:"recorded_by"`
	} `json:"progress"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	ApprovedBy string `json:"approved_by"`
	Revoked    bool   `json:"revoked"`
	RevokedBy  string `json:"revoked_by"`
}

type errorView struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {
	workflow := flag.String("workflow", "", "workflow id to check")
	flag.Parse()

	if err := run(*workflow); err != nil {
		fmt.Fprintln(os.Stderr, "workflow check failed:", err)
		os.Exit(1)
	}
	fmt.Printf("workflow check passed: %s\n", *workflow)
}

func run(workflow string) error {
	handler := httpapi.NewRouter(station.New(repository.NewMemory()))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	switch workflow {
	case "register-specimen":
		return checkRegisterSpecimen(client, server.URL)
	case "approve-prep-plan":
		return checkApprovePrepPlan(client, server.URL)
	case "log-preparation":
		return checkLogPreparation(client, server.URL)
	case "release-for-study":
		return checkReleaseForStudy(client, server.URL)
	case "revoke-release":
		return checkRevokeRelease(client, server.URL)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

func checkRegisterSpecimen(client *http.Client, baseURL string) error {
	body := contracts.RegisterSpecimenRequest{
		CatalogNumber: "PPS-2026-001",
		FieldLabel:    "北坡-07",
		TaxonHint:     "大型脊椎动物",
		MatrixKind:    "粉砂岩",
		JacketNotes:   "夹克边角完好",
	}
	if err := expectError(client, http.MethodPost, baseURL+"/specimens", nil, body, http.StatusForbidden, "actor_name_required"); err != nil {
		return err
	}
	wrongRole := actPreparator
	if err := expectError(client, http.MethodPost, baseURL+"/specimens", &wrongRole, body, http.StatusForbidden, "role_not_permitted"); err != nil {
		return err
	}
	var list struct {
		Items []specimenView `json:"items"`
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens", nil, nil, http.StatusOK, &list); err != nil {
		return err
	}
	if len(list.Items) != 0 {
		return errors.New("unauthorized register attempts changed business state")
	}
	var specimen specimenView
	if err := requestJSON(client, http.MethodPost, baseURL+"/specimens", &actRegistrar, body, http.StatusCreated, &specimen); err != nil {
		return err
	}
	if specimen.Status != "registered" {
		return fmt.Errorf("expected registered status, got %s", specimen.Status)
	}
	if specimen.RegisteredBy != actRegistrar.name {
		return fmt.Errorf("expected registered_by %s, got %s", actRegistrar.name, specimen.RegisteredBy)
	}
	var fetched specimenView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, nil, http.StatusOK, &fetched); err != nil {
		return err
	}
	if fetched.ID != specimen.ID {
		return errors.New("registered specimen cannot be read back")
	}
	return nil
}

func checkApprovePrepPlan(client *http.Client, baseURL string) error {
	specimen, err := registerSpecimen(client, baseURL, "PPS-2026-001")
	if err != nil {
		return err
	}
	wrongRole := actPreparator
	if err := expectError(client, http.MethodPost, baseURL+"/specimens/"+specimen.ID+"/plans", &wrongRole, planBody(), http.StatusForbidden, "role_not_permitted"); err != nil {
		return err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return err
	}
	if plan.CreatedBy != actPlanAuthor.name {
		return fmt.Errorf("expected created_by %s, got %s", actPlanAuthor.name, plan.CreatedBy)
	}
	if err := expectError(client, http.MethodPost, baseURL+"/plans/"+plan.ID+"/approval", &wrongRole, contracts.ApprovalRequest{ApprovedBy: wrongRole.name}, http.StatusForbidden, "role_not_permitted"); err != nil {
		return err
	}
	if err := expectError(client, http.MethodPost, baseURL+"/plans/"+plan.ID+"/approval", &actPlanAuthor, contracts.ApprovalRequest{ApprovedBy: actPlanAuthor.name}, http.StatusForbidden, "self_approval_forbidden"); err != nil {
		return err
	}
	if err := expectError(client, http.MethodPost, baseURL+"/plans/"+plan.ID+"/approval", &actApprover, contracts.ApprovalRequest{ApprovedBy: "someone-else"}, http.StatusForbidden, "actor_mismatch"); err != nil {
		return err
	}
	snapshot, err := getPlanSnapshot(client, baseURL, plan.ID)
	if err != nil {
		return err
	}
	if snapshot.Plan.Status != "draft" {
		return fmt.Errorf("unauthorized approval attempts changed plan state to %s", snapshot.Plan.Status)
	}
	var approved planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		&actApprover,
		contracts.ApprovalRequest{ApprovedBy: actApprover.name},
		http.StatusOK,
		&approved,
	); err != nil {
		return err
	}
	if approved.Status != "approved" || approved.ApprovedBy != actApprover.name {
		return fmt.Errorf("expected approved plan by %s, got %s by %s", actApprover.name, approved.Status, approved.ApprovedBy)
	}
	var fetchedSpecimen specimenView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, nil, http.StatusOK, &fetchedSpecimen); err != nil {
		return err
	}
	if fetchedSpecimen.Status != "preparing" {
		return fmt.Errorf("expected preparing specimen, got %s", fetchedSpecimen.Status)
	}
	return checkConcurrentApproval(client, baseURL)
}

// checkConcurrentApproval races three eligible approvers on one draft plan
// and requires a deterministic outcome: exactly one approval commits and the
// rest conflict, leaving the plan approved exactly once.
func checkConcurrentApproval(client *http.Client, baseURL string) error {
	specimen, err := registerSpecimen(client, baseURL, "PPS-2026-002")
	if err != nil {
		return err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return err
	}
	contenders := []actor{
		actApprover,
		{name: "lead-gao", role: "prep_lead"},
		{name: "lead-qian", role: "prep_lead"},
	}
	statuses := make(chan int, len(contenders))
	var wg sync.WaitGroup
	for _, contender := range contenders {
		wg.Add(1)
		go func(a actor) {
			defer wg.Done()
			status, _, err := doJSON(
				client,
				http.MethodPost,
				baseURL+"/plans/"+plan.ID+"/approval",
				&a,
				contracts.ApprovalRequest{ApprovedBy: a.name},
			)
			if err != nil {
				statuses <- -1
				return
			}
			statuses <- status
		}(contender)
	}
	wg.Wait()
	close(statuses)
	approvedCount, conflictCount := 0, 0
	for status := range statuses {
		switch status {
		case http.StatusOK:
			approvedCount++
		case http.StatusConflict:
			conflictCount++
		default:
			return fmt.Errorf("unexpected concurrent approval status %d", status)
		}
	}
	if approvedCount != 1 || conflictCount != len(contenders)-1 {
		return fmt.Errorf("concurrent approvals not deterministic: %d approved, %d conflicted", approvedCount, conflictCount)
	}
	snapshot, err := getPlanSnapshot(client, baseURL, plan.ID)
	if err != nil {
		return err
	}
	if snapshot.Plan.Status != "approved" {
		return fmt.Errorf("expected approved plan after race, got %s", snapshot.Plan.Status)
	}
	return nil
}

func checkLogPreparation(client *http.Client, baseURL string) error {
	specimen, plan, err := approvedPlanSetup(client, baseURL, "PPS-2026-001")
	if err != nil {
		return err
	}
	approverAsPreparator := actor{name: actApprover.name, role: "preparator"}
	if err := expectError(client, http.MethodPost, baseURL+"/plans/"+plan.ID+"/progress", &approverAsPreparator, progressBody(35, "矩阵剥离", actApprover.name), http.StatusForbidden, "approver_cannot_record"); err != nil {
		return err
	}
	wrongRole := actRegistrar
	if err := expectError(client, http.MethodPost, baseURL+"/plans/"+plan.ID+"/progress", &wrongRole, progressBody(35, "矩阵剥离", wrongRole.name), http.StatusForbidden, "role_not_permitted"); err != nil {
		return err
	}
	snapshot, err := getPlanSnapshot(client, baseURL, plan.ID)
	if err != nil {
		return err
	}
	if snapshot.Plan.Status != "approved" || len(snapshot.Progress) != 0 {
		return errors.New("unauthorized progress attempts changed business state")
	}
	if err := recordProgress(client, baseURL, plan.ID, 35, "矩阵剥离"); err != nil {
		return err
	}
	if err := expectError(client, http.MethodPost, baseURL+"/plans/"+plan.ID+"/progress", &actPreparator, progressBody(30, "回退检查", actPreparator.name), http.StatusConflict, "progress_regressed"); err != nil {
		return err
	}
	if err := recordProgress(client, baseURL, plan.ID, 100, "表面加固"); err != nil {
		return err
	}
	snapshot, err = getPlanSnapshot(client, baseURL, plan.ID)
	if err != nil {
		return err
	}
	if snapshot.Plan.Status != "completed" || len(snapshot.Progress) != 2 {
		return errors.New("plan did not complete with exactly two progress entries")
	}
	if snapshot.Progress[0].RecordedBy != actPreparator.name || snapshot.Progress[1].RecordedBy != actPreparator.name {
		return errors.New("progress entries are not attributed to the recording preparator")
	}
	var fetchedSpecimen specimenView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, nil, http.StatusOK, &fetchedSpecimen); err != nil {
		return err
	}
	if fetchedSpecimen.Status != "stabilized" {
		return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
	}
	return expectError(client, http.MethodPost, baseURL+"/plans/"+plan.ID+"/progress", &actPreparator, progressBody(100, "完成后追加", actPreparator.name), http.StatusConflict, "plan_not_recordable")
}

func checkReleaseForStudy(client *http.Client, baseURL string) error {
	specimen, _, err := completedPlanSetup(client, baseURL, "PPS-2026-001")
	if err != nil {
		return err
	}
	wrongRole := actPreparator
	if err := expectError(client, http.MethodPost, baseURL+"/specimens/"+specimen.ID+"/release", &wrongRole, releaseBody(wrongRole.name), http.StatusForbidden, "role_not_permitted"); err != nil {
		return err
	}
	var fetched specimenView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, nil, http.StatusOK, &fetched); err != nil {
		return err
	}
	if fetched.Status != "stabilized" {
		return fmt.Errorf("unauthorized release attempt changed specimen state to %s", fetched.Status)
	}
	var release releaseView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/release",
		&actCurator,
		releaseBody(actCurator.name),
		http.StatusCreated,
		&release,
	); err != nil {
		return err
	}
	if release.SpecimenID != specimen.ID {
		return errors.New("release record references the wrong specimen")
	}
	if release.ApprovedBy != actCurator.name || release.Revoked {
		return errors.New("release record does not reflect the releasing curator")
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, nil, http.StatusOK, &fetched); err != nil {
		return err
	}
	if fetched.Status != "available" {
		return fmt.Errorf("expected available specimen, got %s", fetched.Status)
	}
	return expectError(client, http.MethodPost, baseURL+"/specimens/"+specimen.ID+"/release", &actCurator, releaseBody(actCurator.name), http.StatusConflict, "release_exists")
}

func checkRevokeRelease(client *http.Client, baseURL string) error {
	specimen, _, err := releasedSetup(client, baseURL, "PPS-2026-001")
	if err != nil {
		return err
	}
	wrongRole := actPreparator
	if err := expectError(client, http.MethodPost, baseURL+"/specimens/"+specimen.ID+"/release/revocation", &wrongRole, contracts.RevokeReleaseRequest{Reason: "越权测试"}, http.StatusForbidden, "role_not_permitted"); err != nil {
		return err
	}
	var fetched specimenView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, nil, http.StatusOK, &fetched); err != nil {
		return err
	}
	if fetched.Status != "available" {
		return fmt.Errorf("unauthorized revocation attempt changed specimen state to %s", fetched.Status)
	}
	var revoked releaseView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/release/revocation",
		&actCurator,
		contracts.RevokeReleaseRequest{Reason: "借阅方取消研究计划"},
		http.StatusOK,
		&revoked,
	); err != nil {
		return err
	}
	if !revoked.Revoked || revoked.RevokedBy != actCurator.name {
		return errors.New("release record was not revoked by the curator")
	}
	if revoked.ApprovedBy != actCurator.name || revoked.ID == "" {
		return errors.New("original release record was not preserved")
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, nil, http.StatusOK, &fetched); err != nil {
		return err
	}
	if fetched.Status != "stabilized" {
		return fmt.Errorf("expected stabilized specimen after revocation, got %s", fetched.Status)
	}
	if err := expectError(client, http.MethodPost, baseURL+"/specimens/"+specimen.ID+"/release/revocation", &actCurator, contracts.RevokeReleaseRequest{}, http.StatusConflict, "release_already_revoked"); err != nil {
		return err
	}
	if err := expectError(client, http.MethodPost, baseURL+"/specimens/"+specimen.ID+"/release", &actCurator, releaseBody(actCurator.name), http.StatusConflict, "release_exists"); err != nil {
		return err
	}
	fresh, err := registerSpecimen(client, baseURL, "PPS-2026-002")
	if err != nil {
		return err
	}
	return expectError(client, http.MethodPost, baseURL+"/specimens/"+fresh.ID+"/release/revocation", &actCurator, contracts.RevokeReleaseRequest{}, http.StatusNotFound, "release_not_found")
}

func planBody() contracts.CreatePlanRequest {
	return contracts.CreatePlanRequest{
		Objective:      "暴露主要关节面",
		Method:         "手工显微剥离",
		Adhesive:       "可逆树脂",
		TargetExposure: 80,
	}
}

func progressBody(percent int, stage string, recordedBy string) contracts.ProgressRequest {
	return contracts.ProgressRequest{
		Stage:      stage,
		Percent:    percent,
		Note:       "本地链路检查",
		RecordedBy: recordedBy,
	}
}

func releaseBody(approvedBy string) contracts.ReleaseRequest {
	return contracts.ReleaseRequest{
		Purpose:    "比较解剖研究",
		Recipient:  "地层研究组",
		ApprovedBy: approvedBy,
	}
}

func registerSpecimen(client *http.Client, baseURL string, catalog string) (specimenView, error) {
	var specimen specimenView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		&actRegistrar,
		contracts.RegisterSpecimenRequest{
			CatalogNumber: catalog,
			FieldLabel:    "北坡-07",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
		},
		http.StatusCreated,
		&specimen,
	)
	return specimen, err
}

func createPlan(client *http.Client, baseURL, specimenID string) (planView, error) {
	var plan planView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/plans",
		&actPlanAuthor,
		planBody(),
		http.StatusCreated,
		&plan,
	)
	return plan, err
}

func getPlanSnapshot(client *http.Client, baseURL, planID string) (planSnapshot, error) {
	var snapshot planSnapshot
	err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+planID, nil, nil, http.StatusOK, &snapshot)
	return snapshot, err
}

func approvedPlanSetup(client *http.Client, baseURL string, catalog string) (specimenView, planView, error) {
	specimen, err := registerSpecimen(client, baseURL, catalog)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	var approved planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		&actApprover,
		contracts.ApprovalRequest{ApprovedBy: actApprover.name},
		http.StatusOK,
		&approved,
	); err != nil {
		return specimenView{}, planView{}, err
	}
	return specimen, approved, nil
}

func completedPlanSetup(client *http.Client, baseURL string, catalog string) (specimenView, planView, error) {
	specimen, plan, err := approvedPlanSetup(client, baseURL, catalog)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	if err := recordProgress(client, baseURL, plan.ID, 100, "表面加固"); err != nil {
		return specimenView{}, planView{}, err
	}
	return specimen, plan, nil
}

func releasedSetup(client *http.Client, baseURL string, catalog string) (specimenView, releaseView, error) {
	specimen, _, err := completedPlanSetup(client, baseURL, catalog)
	if err != nil {
		return specimenView{}, releaseView{}, err
	}
	var release releaseView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/release",
		&actCurator,
		releaseBody(actCurator.name),
		http.StatusCreated,
		&release,
	); err != nil {
		return specimenView{}, releaseView{}, err
	}
	return specimen, release, nil
}

func recordProgress(client *http.Client, baseURL, planID string, percent int, stage string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/progress",
		&actPreparator,
		progressBody(percent, stage, actPreparator.name),
		http.StatusCreated,
		&ignored,
	)
}

func doJSON(
	client *http.Client,
	method string,
	url string,
	act *actor,
	body any,
) (int, []byte, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, payload)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if act != nil {
		request.Header.Set("X-Actor-Name", act.name)
		request.Header.Set("X-Actor-Role", act.role)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, raw, nil
}

func requestJSON(
	client *http.Client,
	method string,
	url string,
	act *actor,
	body any,
	wantStatus int,
	target any,
) error {
	status, raw, err := doJSON(client, method, url, act, body)
	if err != nil {
		return err
	}
	if status != wantStatus {
		return fmt.Errorf("request %s %s returned %d: %s", method, url, status, string(raw))
	}
	if target == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode response for %s %s: %w", method, url, err)
	}
	return nil
}

func expectError(
	client *http.Client,
	method string,
	url string,
	act *actor,
	body any,
	wantStatus int,
	wantCode string,
) error {
	status, raw, err := doJSON(client, method, url, act, body)
	if err != nil {
		return err
	}
	if status != wantStatus {
		return fmt.Errorf("request %s %s returned %d, want %d: %s", method, url, status, wantStatus, string(raw))
	}
	var view errorView
	if err := json.Unmarshal(raw, &view); err != nil {
		return fmt.Errorf("decode error response for %s %s: %w", method, url, err)
	}
	if view.Code != wantCode {
		return fmt.Errorf("request %s %s returned code %q, want %q", method, url, view.Code, wantCode)
	}
	return nil
}
