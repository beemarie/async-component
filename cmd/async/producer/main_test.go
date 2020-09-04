package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAsyncRequestHeader(t *testing.T) {
	testserver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "POST" {
			t.Errorf("Expected 'POST' OR 'GET' request, got '%s'", r.Method)
		}
	}))

	tests := []struct {
		name       string
		async      bool
		largeBody  bool
		returncode int
	}{{
		name:       "async request",
		async:      true,
		largeBody:  false,
		returncode: 500, //TODO: how can we test 202 return without standing up redis?
	}, {
		name:       "non async request",
		async:      false,
		largeBody:  false,
		returncode: 200,
	}, {
		name:       "async post request with too large payload",
		async:      true,
		largeBody:  true,
		returncode: 500,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodGet, testserver.URL, nil)
			if test.async {
				request.Header.Set("Prefer", "respond-async")
			}
			if test.largeBody {
				request.Header.Set("Content-Length", "70000000")
			}
			rr := httptest.NewRecorder()

			checkHeaderAndServe(rr, request)

			got := rr.Code
			want := test.returncode

			if got != want {
				t.Errorf("got %d, want %d", got, want)
			}
		})
	}
}
