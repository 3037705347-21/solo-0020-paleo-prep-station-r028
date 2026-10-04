package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

const (
	opRegistrar    = "registrar-song"
	opLeadCreator  = "lead-wang"
	opLeadApprover = "lead-zhao"
	opPreparator   = "preparator-chen"
	opCurator      = "keeper-zhou"
)

type specimenView struct {
	ID            string `json:"id"`
	CatalogNumber string `json:"catalog_number"`
	Status        string `json:"status"`
}

type specimenListView struct {
	Items []specimenView `json:"items"`
}

type planView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	Status     string `json:"status"`
}

type planSnapshot struct {
	Plan     planView `json:"plan"`
	Progress []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
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
	service := station.New(
		repository.NewMemory(),
		station.WithOperators(station.DefaultOperators()...),
	)
	server := httptest.NewServer(httpapi.NewRouter(service))
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	ctx := context.Background()

	switch workflow {
	case "register-specimen":
		specimen, err := registerSpecimen(client, server.URL, "PPS-2026-001", opRegistrar)
		if err != nil {
			return err
		}
		if specimen.Status != "registered" {
			return fmt.Errorf("expected registered status, got %s", specimen.Status)
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.ID != specimen.ID {
			return errors.New("registered specimen cannot be read back")
		}
		if err := expectRegisterRejected(client, server.URL, "PPS-2026-001", opRegistrar, http.StatusConflict); err != nil {
			return err
		}
		if err := expectRegisterRejected(client, server.URL, "PPS-2026-002", opPreparator, http.StatusForbidden); err != nil {
			return err
		}
		if err := expectRegisterRejected(client, server.URL, "PPS-2026-002", "ghost-operator", http.StatusForbidden); err != nil {
			return err
		}
		count, err := specimenCount(client, server.URL)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("rejected registrations changed state, specimen count is %d", count)
		}
		if err := service.AssignRole(ctx, opPreparator, domain.RoleRegistrar); err != nil {
			return err
		}
		promoted, err := registerSpecimen(client, server.URL, "PPS-2026-002", opPreparator)
		if err != nil {
			return err
		}
		if promoted.Status != "registered" {
			return fmt.Errorf("expected registered status after role change, got %s", promoted.Status)
		}
		return nil

	case "approve-prep-plan":
		specimen, err := registerSpecimen(client, server.URL, "PPS-2026-001", opRegistrar)
		if err != nil {
			return err
		}
		plan, err := createPlan(client, server.URL, specimen.ID, opLeadCreator)
		if err != nil {
			return err
		}
		if err := expectApproveRejected(client, server.URL, plan.ID, opLeadCreator, http.StatusForbidden); err != nil {
			return err
		}
		if err := expectApproveRejected(client, server.URL, plan.ID, opPreparator, http.StatusForbidden); err != nil {
			return err
		}
		if err := expectPlanState(client, server.URL, plan.ID, "draft", 0); err != nil {
			return err
		}
		approved, err := approvePlan(client, server.URL, plan.ID, opLeadApprover)
		if err != nil {
			return err
		}
		if approved.Status != "approved" {
			return fmt.Errorf("expected approved plan, got %s", approved.Status)
		}
		if err := expectApproveRejected(client, server.URL, plan.ID, opLeadApprover, http.StatusConflict); err != nil {
			return err
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "preparing" {
			return fmt.Errorf("expected preparing specimen, got %s", fetchedSpecimen.Status)
		}
		return checkPlanCancellation(client, server.URL)

	case "log-preparation":
		specimen, plan, err := approvedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := expectProgressRejected(client, server.URL, plan.ID, 35, opLeadApprover, http.StatusForbidden); err != nil {
			return err
		}
		if err := expectPlanState(client, server.URL, plan.ID, "approved", 0); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 35, "基质剥离"); err != nil {
			return err
		}
		if err := expectProgressRejected(client, server.URL, plan.ID, 30, opPreparator, http.StatusConflict); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		if err := expectPlanState(client, server.URL, plan.ID, "completed", 2); err != nil {
			return err
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "stabilized" {
			return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
		}
		return expectProgressRejected(client, server.URL, plan.ID, 100, opPreparator, http.StatusConflict)

	case "release-for-study":
		specimen, plan, err := approvedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		if err := expectReleaseRejected(client, server.URL, specimen.ID, opLeadCreator, http.StatusForbidden); err != nil {
			return err
		}
		if err := service.AssignRole(ctx, opPreparator, domain.RoleCurator); err != nil {
			return err
		}
		if err := expectReleaseRejected(client, server.URL, specimen.ID, opPreparator, http.StatusForbidden); err != nil {
			return err
		}
		release, err := releaseSpecimen(client, server.URL, specimen.ID, opCurator)
		if err != nil {
			return err
		}
		if release.SpecimenID != specimen.ID {
			return errors.New("release record references the wrong specimen")
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.Status != "available" {
			return fmt.Errorf("expected available specimen, got %s", fetched.Status)
		}
		return expectReleaseRejected(client, server.URL, specimen.ID, opCurator, http.StatusConflict)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

// checkPlanCancellation exercises the cancellation boundary on a second
// specimen: only the plan creator may cancel a draft, and a cancelled draft
// frees the specimen for a fresh plan.
func checkPlanCancellation(client *http.Client, baseURL string) error {
	specimen, err := registerSpecimen(client, baseURL, "PPS-2026-101", opRegistrar)
	if err != nil {
		return err
	}
	plan, err := createPlan(client, baseURL, specimen.ID, opLeadApprover)
	if err != nil {
		return err
	}
	if err := expectCancelRejected(client, baseURL, plan.ID, opLeadCreator, http.StatusForbidden); err != nil {
		return err
	}
	if err := expectCancelRejected(client, baseURL, plan.ID, opPreparator, http.StatusForbidden); err != nil {
		return err
	}
	if err := expectPlanState(client, baseURL, plan.ID, "draft", 0); err != nil {
		return err
	}
	var cancelled planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/cancellation",
		contracts.CancelPlanRequest{CancelledBy: opLeadApprover},
		http.StatusOK,
		&cancelled,
	); err != nil {
		return err
	}
	if cancelled.Status != "cancelled" {
		return fmt.Errorf("expected cancelled plan, got %s", cancelled.Status)
	}
	if _, err := createPlan(client, baseURL, specimen.ID, opLeadCreator); err != nil {
		return err
	}
	return nil
}

func registerSpecimen(client *http.Client, baseURL, catalog, registeredBy string) (specimenView, error) {
	var specimen specimenView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: catalog,
			FieldLabel:    "北坡-07",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
			RegisteredBy:  registeredBy,
		},
		http.StatusCreated,
		&specimen,
	)
	return specimen, err
}

func expectRegisterRejected(client *http.Client, baseURL, catalog, registeredBy string, wantStatus int) error {
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: catalog,
			FieldLabel:    "北坡-07",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
			RegisteredBy:  registeredBy,
		},
		wantStatus,
		nil,
	)
}

func specimenCount(client *http.Client, baseURL string) (int, error) {
	var list specimenListView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens", nil, http.StatusOK, &list); err != nil {
		return 0, err
	}
	return len(list.Items), nil
}

func createPlan(client *http.Client, baseURL, specimenID, createdBy string) (planView, error) {
	var plan planView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/plans",
		contracts.CreatePlanRequest{
			Objective:      "暴露主要关节面",
			Method:         "手工显微剥离",
			Adhesive:       "可逆树脂",
			TargetExposure: 80,
			CreatedBy:      createdBy,
		},
		http.StatusCreated,
		&plan,
	)
	return plan, err
}

func approvePlan(client *http.Client, baseURL, planID, approvedBy string) (planView, error) {
	var plan planView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: approvedBy},
		http.StatusOK,
		&plan,
	)
	return plan, err
}

func expectApproveRejected(client *http.Client, baseURL, planID, approvedBy string, wantStatus int) error {
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: approvedBy},
		wantStatus,
		nil,
	)
}

func expectCancelRejected(client *http.Client, baseURL, planID, cancelledBy string, wantStatus int) error {
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/cancellation",
		contracts.CancelPlanRequest{CancelledBy: cancelledBy},
		wantStatus,
		nil,
	)
}

func expectPlanState(client *http.Client, baseURL, planID, wantStatus string, wantEntries int) error {
	var snapshot planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+planID, nil, http.StatusOK, &snapshot); err != nil {
		return err
	}
	if snapshot.Plan.Status != wantStatus {
		return fmt.Errorf("expected plan status %s, got %s", wantStatus, snapshot.Plan.Status)
	}
	if len(snapshot.Progress) != wantEntries {
		return fmt.Errorf("expected %d progress entries, got %d", wantEntries, len(snapshot.Progress))
	}
	return nil
}

func approvedPlanSetup(client *http.Client, baseURL string) (specimenView, planView, error) {
	specimen, err := registerSpecimen(client, baseURL, "PPS-2026-001", opRegistrar)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	plan, err := createPlan(client, baseURL, specimen.ID, opLeadCreator)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	approved, err := approvePlan(client, baseURL, plan.ID, opLeadApprover)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	return specimen, approved, nil
}

func recordProgress(client *http.Client, baseURL, planID string, percent int, stage string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/progress",
		contracts.ProgressRequest{
			Stage:      stage,
			Percent:    percent,
			Note:       "本地链路检查",
			RecordedBy: opPreparator,
		},
		http.StatusCreated,
		&ignored,
	)
}

func expectProgressRejected(client *http.Client, baseURL, planID string, percent int, recordedBy string, wantStatus int) error {
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/progress",
		contracts.ProgressRequest{
			Stage:      "越权探针",
			Percent:    percent,
			Note:       "本地链路检查",
			RecordedBy: recordedBy,
		},
		wantStatus,
		nil,
	)
}

func releaseSpecimen(client *http.Client, baseURL, specimenID, approvedBy string) (releaseView, error) {
	var release releaseView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/release",
		contracts.ReleaseRequest{
			Purpose:    "比较解剖研究",
			Recipient:  "地层研究组",
			ApprovedBy: approvedBy,
		},
		http.StatusCreated,
		&release,
	)
	return release, err
}

func expectReleaseRejected(client *http.Client, baseURL, specimenID, approvedBy string, wantStatus int) error {
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/release",
		contracts.ReleaseRequest{
			Purpose:    "比较解剖研究",
			Recipient:  "地层研究组",
			ApprovedBy: approvedBy,
		},
		wantStatus,
		nil,
	)
}

func requestJSON(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
	target any,
) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, payload)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != wantStatus {
		return fmt.Errorf("request %s %s returned %d: %s", method, url, response.StatusCode, string(raw))
	}
	if target == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode response for %s %s: %w", method, url, err)
	}
	return nil
}
