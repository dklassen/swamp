package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
)

func TestApplicationWrites_SendTheChangeFromTheAsker(t *testing.T) {
	t.Parallel()

	src := message.Source{Instance: 7}
	tests := []struct {
		name string
		// started is zero for a posting with no application yet. write's
		// source is the asker the change should come from.
		write func(s testutil.TestApp, postingID int64, started message.Application) (tea.Cmd, message.Source)
		op    message.Op
		check func(t *testing.T, started message.Application, c message.Changed[message.Application])
	}{
		{
			name: "create",
			write: func(s testutil.TestApp, postingID int64, _ message.Application) (tea.Cmd, message.Source) {
				return createApplication(s.Postings, src, postingID), src
			},
			op: message.OpCreated,
			check: func(t *testing.T, _ message.Application, c message.Changed[message.Application]) {
				if c.Before != (message.Application{}) || c.After.Status != message.ApplicationStarted {
					t.Errorf("Before, After.Status = %+v, %v, want zero, %v", c.Before, c.After.Status, message.ApplicationStarted)
				}
			},
		},
		{
			name: "status",
			write: func(s testutil.TestApp, _ int64, started message.Application) (tea.Cmd, message.Source) {
				return updateApplicationStatus(s.Applications, src, started.ID, application.Interviewing), src
			},
			op: message.OpUpdated,
			check: func(t *testing.T, started message.Application, c message.Changed[message.Application]) {
				if c.Before != started || c.After.Status != message.ApplicationInterviewing {
					t.Errorf("Before, After.Status = %+v, %v, want %+v, %v", c.Before, c.After.Status, started, message.ApplicationInterviewing)
				}
			},
		},
		{
			name: "notes",
			write: func(s testutil.TestApp, _ int64, started message.Application) (tea.Cmd, message.Source) {
				return savedNotes(s.Applications, started, "call Tuesday")
			},
			op: message.OpUpdated,
			check: func(t *testing.T, started message.Application, c message.Changed[message.Application]) {
				if c.Before != started || c.After.Notes != "call Tuesday" {
					t.Errorf("Before, After.Notes = %+v, %q, want %+v, %q", c.Before, c.After.Notes, started, "call Tuesday")
				}
			},
		},
		{
			name: "delete",
			write: func(s testutil.TestApp, _ int64, started message.Application) (tea.Cmd, message.Source) {
				return deleteApplication(s.Applications, src, started.ID), src
			},
			op: message.OpDeleted,
			check: func(t *testing.T, started message.Application, c message.Changed[message.Application]) {
				if c.Before != started || c.After != (message.Application{}) {
					t.Errorf("Before, After = %+v, %+v, want %+v, zero", c.Before, c.After, started)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			p := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
			var started message.Application
			if tt.op != message.OpCreated {
				started = applicationSnapshot(mustCreateApplication(t, s, p.ID))
			}

			cmd, asker := tt.write(s, p.ID, started)
			got := runCmd(t, cmd)
			if len(got) != 1 {
				t.Fatalf("sent %#v, want one change", got)
			}
			e, ok := got[0].(message.Event[message.Changed[message.Application]])
			if !ok {
				t.Fatalf("sent %T, want message.Event[message.Changed[message.Application]]", got[0])
			}
			assertEventFrom(t, e.Meta, asker)
			id := started.ID
			if tt.op == message.OpCreated {
				id = e.Payload.After.ID
			}
			if want := (message.Subject{Type: message.TypeApplication, ID: id}); id == 0 || e.Meta.Subject != want || e.Meta.Op != tt.op {
				t.Errorf("Subject, Op = %+v, %v, want %+v, %v", e.Meta.Subject, e.Meta.Op, want, tt.op)
			}
			tt.check(t, started, e.Payload)
		})
	}
}

func TestApplicationWrites_ThatFail_SendAnErrorAboutTheirSubject(t *testing.T) {
	t.Parallel()

	src := message.Source{Instance: 7}
	const missing = 999
	tests := []struct {
		name string
		// write gets a posting that's open, or closed when closed is set,
		// and returns the asker the error should come from too.
		write    func(s testutil.TestApp, postingID int64) (tea.Cmd, message.Source)
		closed   bool
		op       message.Op
		wantType message.Type
		wantID   func(postingID int64) int64
		wantCode message.ErrorCode
	}{
		{
			name: "create on a closed posting",
			write: func(s testutil.TestApp, postingID int64) (tea.Cmd, message.Source) {
				return createApplication(s.Postings, src, postingID), src
			},
			closed:   true,
			op:       message.OpCreated,
			wantType: message.TypePosting,
			wantID:   func(postingID int64) int64 { return postingID },
			wantCode: message.ErrPostingClosed,
		},
		{
			name: "create on a missing posting",
			write: func(s testutil.TestApp, _ int64) (tea.Cmd, message.Source) {
				return createApplication(s.Postings, src, missing), src
			},
			op:       message.OpCreated,
			wantType: message.TypePosting,
			wantID:   func(int64) int64 { return missing },
			wantCode: message.ErrNotFound,
		},
		{
			name: "status of a missing application",
			write: func(s testutil.TestApp, _ int64) (tea.Cmd, message.Source) {
				return updateApplicationStatus(s.Applications, src, missing, application.Interviewing), src
			},
			op:       message.OpUpdated,
			wantType: message.TypeApplication,
			wantID:   func(int64) int64 { return missing },
			wantCode: message.ErrNotFound,
		},
		{
			name: "notes of a missing application",
			write: func(s testutil.TestApp, _ int64) (tea.Cmd, message.Source) {
				return savedNotes(s.Applications, message.Application{ID: missing}, "call Tuesday")
			},
			op:       message.OpUpdated,
			wantType: message.TypeApplication,
			wantID:   func(int64) int64 { return missing },
			wantCode: message.ErrNotFound,
		},
		{
			name: "delete of a missing application",
			write: func(s testutil.TestApp, _ int64) (tea.Cmd, message.Source) {
				return deleteApplication(s.Applications, src, missing), src
			},
			op:       message.OpDeleted,
			wantType: message.TypeApplication,
			wantID:   func(int64) int64 { return missing },
			wantCode: message.ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			p := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
			if tt.closed {
				if err := s.Postings.Close(context.Background(), p.ID, nil); err != nil {
					t.Fatalf("Close: %v", err)
				}
			}

			cmd, asker := tt.write(s, p.ID)
			got := runCmd(t, cmd)
			if len(got) != 1 {
				t.Fatalf("sent %#v, want one error", got)
			}
			e, ok := got[0].(message.Event[message.Error])
			if !ok {
				t.Fatalf("sent %T, want message.Event[message.Error]", got[0])
			}
			assertEventFrom(t, e.Meta, asker)
			want := message.Subject{Type: tt.wantType, ID: tt.wantID(p.ID)}
			if e.Meta.Subject != want || e.Meta.Op != tt.op || e.Payload.Code != tt.wantCode {
				t.Errorf("Subject, Op, Code = %+v, %v, %v, want %+v, %v, %v", e.Meta.Subject, e.Meta.Op, e.Payload.Code, want, tt.op, tt.wantCode)
			}
		})
	}
}

// savedNotes is notes typed into an editor on app and saved, and the
// editor, which asked.
func savedNotes(applications *service.ApplicationService, app message.Application, notes string) (tea.Cmd, message.Source) {
	m := newApplicationNotesModel(applications, app, message.ContentSize{Width: 80, Rows: 10})
	m.textarea.SetValue(notes)
	return m.keyPressed(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}), m.src()
}
