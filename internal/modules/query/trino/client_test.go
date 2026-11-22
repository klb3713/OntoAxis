package trino

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientFollowsNextURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/statement":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"id":"q1","columns":[{"name":"x","type":"integer"}],"data":[[1]],"nextUri":"http://%s/next"}`, r.Host)
		case "/next":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"q1","columns":[{"name":"x","type":"integer"}],"data":[[2]],"nextUri":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := New(srv.URL, "tester", 5e9)
	result, err := client.Execute(context.Background(), "SELECT 1", 10)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.RowCount != 2 || len(result.Columns) != 1 || result.Columns[0] != "x" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestClientTrinoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"q1","error":{"message":"syntax error","errorCode":1}}`))
	}))
	defer srv.Close()
	client := New(srv.URL, "tester", 5e9)
	if _, err := client.Execute(context.Background(), "SELECT", 10); err == nil {
		t.Fatal("expected error")
	}
}
