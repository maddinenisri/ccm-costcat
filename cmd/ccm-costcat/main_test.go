package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/maddinenisri/ccm-costcat/internal/harness"
)

func TestListNames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pages []string
		want  string
	}{
		{
			name: "all pages with duplicate names",
			pages: []string{
				`{"resource":{"totalCount":2,"businessMappings":[{"uuid":"first","name":"Teams","accountId":"ACC","costTargets":[{"name":"Platform","rules":[]}]}]}}`,
				`{"resource":{"totalCount":2,"businessMappings":[{"uuid":"second","name":"Teams","accountId":"ACC","dataSources":["AWS"]}]}}`,
			},
			want: `[{"uuid":"first","name":"Teams"},{"uuid":"second","name":"Teams"}]`,
		},
		{
			name:  "empty result is an array",
			pages: []string{`{"resource":{"totalCount":0,"businessMappings":[]}}`},
			want:  `[]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var offsets []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/ccm/api/business-mapping" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				q := r.URL.Query()
				if q.Get("accountIdentifier") != "ACC" || q.Get("limit") != "1" || q.Get("searchKey") != "Teams" {
					t.Errorf("unexpected query: %s", r.URL.RawQuery)
				}
				if r.Header.Get("x-api-key") != "test-key" {
					t.Error("missing API key")
				}
				page := len(offsets)
				offsets = append(offsets, q.Get("offset"))
				if page >= len(tc.pages) {
					t.Error("requested an unexpected page")
					http.Error(w, "unexpected page", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.pages[page]))
			}))
			defer server.Close()

			client, err := harness.NewClient("test-key", "ACC", harness.WithBaseURL(server.URL), harness.WithMaxRetries(0))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			runList(context.Background(), client, common{format: "names", outDir: dir}, harness.ListOptions{Limit: 1, SearchKey: "Teams"})

			if len(offsets) != len(tc.pages) || offsets[0] != "" || (len(offsets) > 1 && offsets[1] != "1") {
				t.Fatalf("unexpected pagination offsets: %v", offsets)
			}
			data, err := os.ReadFile(filepath.Join(dir, "cost_categories_names.json"))
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected names output: %s", data)
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 {
				t.Fatalf("expected only the names output file, got %d files", len(files))
			}
		})
	}
}
