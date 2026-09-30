package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/sync"
)

func TestReportFetch(t *testing.T) {
	t.Parallel()

	names := map[int64]string{1: "Acme", 2: "Globex", 3: "Initech"}

	tests := []struct {
		name       string
		results    []sync.Result
		wantFailed int
		wantStdout string
		wantStderr string
	}{
		{
			name: "all succeed",
			results: []sync.Result{
				{CompanyID: 1, Fetched: 3, Created: 1},
				{CompanyID: 2, Fetched: 2, Updated: 2},
			},
			wantFailed: 0,
			wantStdout: "Acme: fetched=3 created=1 updated=0 closed=0 reopened=0\n" +
				"Globex: fetched=2 created=0 updated=2 closed=0 reopened=0\n",
			wantStderr: "2 companies, 0 failed\n",
		},
		{
			name: "some fail",
			results: []sync.Result{
				{CompanyID: 1, Fetched: 3, Created: 1},
				{CompanyID: 2, Err: errors.New("status 404")},
				{CompanyID: 3, Err: errors.New("context deadline exceeded")},
			},
			wantFailed: 2,
			wantStdout: "Acme: fetched=3 created=1 updated=0 closed=0 reopened=0\n",
			wantStderr: "Globex: error: status 404\n" +
				"Initech: error: context deadline exceeded\n" +
				"3 companies, 2 failed (Globex, Initech)\n",
		},
		{
			// A company another sync was already running for isn't a
			// failure: it's being synced, just not by this run (#150).
			name: "one skipped because another sync holds it",
			results: []sync.Result{
				{CompanyID: 1, Fetched: 3, Created: 1},
				{CompanyID: 2, Err: sync.ErrSyncInProgress},
				{CompanyID: 3, Err: errors.New("status 404")},
			},
			wantFailed: 1,
			wantStdout: "Acme: fetched=3 created=1 updated=0 closed=0 reopened=0\n",
			wantStderr: "Globex: skipped, another sync of it is in progress\n" +
				"Initech: error: status 404\n" +
				"3 companies, 1 failed (Initech), 1 skipped (Globex)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			failed := reportFetch(&stdout, &stderr, tt.results, names)

			if failed != tt.wantFailed {
				t.Errorf("failed = %d, want %d", failed, tt.wantFailed)
			}
			if diff := cmp.Diff(tt.wantStdout, stdout.String()); diff != "" {
				t.Errorf("stdout mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantStderr, stderr.String()); diff != "" {
				t.Errorf("stderr mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
