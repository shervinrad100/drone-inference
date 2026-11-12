package main

import (
	"bytes"
	"drone-target-detection/src/internal/svc"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// the purpose is to just test the response shape

func TestRegisterServiceHandler(t *testing.T) {
	s := &server{registry: newRegistry()}

	// build the request and response writer
	testServiceName := "video-receiver"
	testAddress := "0.0.0.1:8080"
	regInstanceReq := svc.RegisterInstanceRequest{Address: testAddress}
	jsonBytes, _ := json.Marshal(regInstanceReq)

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/services/%s/instances", testServiceName),
		bytes.NewReader(jsonBytes),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	// make the call
	s.routes().ServeHTTP(w, req)

	// check status code
	expectedCode := http.StatusCreated
	if w.Code != expectedCode {
		t.Errorf("bad response code, expected: %v but got %v\nbody: %s\n",
			expectedCode, w.Code, w.Body.String())
	}

	// check the response shape
	var resp svc.Instance
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to unmarshal response into an Instance: %v", err)
	}

	if resp.Address == "" {
		t.Errorf("failed to create the instance, instance: %v", resp)
	}

}

func TestRegisterServiceHandlerWithBadRequest(t *testing.T) {
	// TODO what if field names are wrong
	// TODO what if values are malformed
	// TODO what if there are missing fields?
}
