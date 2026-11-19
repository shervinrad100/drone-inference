package main

import (
	"bytes"
	"drone-inference/src/internal/svc"
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
	tests := []struct {
		name      string
		serviceId string
		request   any
		wantErr   bool
	}{
		{"bad field name", "video-receiver", struct{ foo string }{foo: "0.0.0.1:8080"}, true},
		{"bad json value", "video-receiver", struct{ Address int }{Address: 123}, true},
		{"missing fields", "video-receiver", struct{}{}, true},
	}

	// set up server
	s := &server{registry: newRegistry()}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/services/%s/instances", tt.serviceId),
				bytes.NewReader(jsonBytes),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			s.routes().ServeHTTP(w, req)

			// check status code
			expectedCode := http.StatusBadRequest
			if tt.wantErr {
				if w.Code != expectedCode {
					t.Errorf("bad response code, expected: %v but got %v\nbody: %s\n",
						expectedCode, w.Code, w.Body.String())
				}
				return
			}

		})
	}

}
