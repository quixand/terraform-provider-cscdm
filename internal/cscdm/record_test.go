package cscdm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"terraform-provider-cscdm/internal/cscdm"
	"testing"
)

func TestClient_PerformRecordAction_SurfacesFailedZoneEditMessage(t *testing.T) {
	const failureMessage = "duplicate record: a CNAME with this key already exists"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/zones/edits":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]string{"status": "PENDING", "message": ""},
				"links": map[string]string{
					"self":   "/api/v2/zones/edits/edit-1",
					"status": "/api/v2/zones/edits/status/edit-1",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/zones/edits/status/edit-1":
			json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]string{"status": "FAILED", "message": failureMessage},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v2/zones/edits/edit-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &cscdm.Client{}
	client.Configure("test-key", "test-token", server.URL+"/api/v2/")
	defer client.Stop()

	_, err := client.PerformRecordAction(&cscdm.RecordAction{
		ZoneEdit: cscdm.ZoneEdit{Action: "ADD", RecordType: "CNAME", NewKey: "www", NewValue: "example.com", NewTtl: 300},
		ZoneName: "example.com",
	})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), failureMessage) {
		t.Errorf("expected error to contain %q, got: %s", failureMessage, err.Error())
	}
}

func TestClient_PerformRecordAction_SurfacesCreateErrorDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v2/zones/edits" {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"code":        "RECORD_ALREADY_EXISTS",
				"description": "a record with this key already exists",
				"value":       "www",
			})
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &cscdm.Client{}
	client.Configure("test-key", "test-token", server.URL+"/api/v2/")
	defer client.Stop()

	_, err := client.PerformRecordAction(&cscdm.RecordAction{
		ZoneEdit: cscdm.ZoneEdit{Action: "ADD", RecordType: "CNAME", NewKey: "www", NewValue: "example.com", NewTtl: 300},
		ZoneName: "example.com",
	})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	for _, want := range []string{"RECORD_ALREADY_EXISTS", "a record with this key already exists", "www"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to contain %q, got: %s", want, err.Error())
		}
	}
}

func TestClient_GetRecordByTypeByKey_FindsExistingRecord(t *testing.T) {
	client := &cscdm.Client{}
	zone := &cscdm.Zone{
		ZoneName: "example.com",
		CNAME: []cscdm.ZoneRecord{
			{Id: "rec-1", Key: "www", Value: "target.example.com", Status: "ACTIVE"},
		},
	}

	record, err := client.GetRecordByTypeByKey(zone, "CNAME", "www")
	if err != nil {
		t.Fatalf("expected to find existing record, got error: %s", err)
	}
	if record.Id != "rec-1" {
		t.Errorf("expected id %q, got %q", "rec-1", record.Id)
	}

	if _, err := client.GetRecordByTypeByKey(zone, "CNAME", "missing"); err == nil {
		t.Error("expected error for a key with no matching record, got nil")
	}
}

func TestClient_PerformRecordAction_SendsChangeIdAndComments(t *testing.T) {
	bodies := make(chan map[string]any, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/zones/edits":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			bodies <- body
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]string{"status": "PENDING", "message": ""},
				"links": map[string]string{
					"self":   "/api/v2/zones/edits/edit-1",
					"status": "/api/v2/zones/edits/status/edit-1",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/zones/edits/status/edit-1":
			json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]string{"status": "FAILED", "message": "stop here"},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v2/zones/edits/edit-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &cscdm.Client{}
	client.Configure("test-key", "test-token", server.URL+"/api/v2/")
	defer client.Stop()

	client.PerformRecordAction(&cscdm.RecordAction{
		ZoneEdit: cscdm.ZoneEdit{
			Action: "ADD", RecordType: "CNAME", NewKey: "www", NewValue: "example.com", NewTtl: 300,
			ChangeId: "chg-1", Comments: "a comment",
		},
		ZoneName: "example.com",
	})

	body := <-bodies
	edit := body["edits"].([]any)[0].(map[string]any)
	if edit["changeId"] != "chg-1" {
		t.Errorf("expected changeId chg-1, got %v", edit["changeId"])
	}
	if edit["comments"] != "a comment" {
		t.Errorf("expected comments %q, got %v", "a comment", edit["comments"])
	}
}

func TestZoneEdit_OmitsEmptyChangeIdAndComments(t *testing.T) {
	b, err := json.Marshal(cscdm.ZoneEdit{Action: "ADD", RecordType: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "changeId") || strings.Contains(string(b), "comments") {
		t.Errorf("expected changeId/comments to be omitted, got %s", b)
	}
}
