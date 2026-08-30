package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateCompany_ThenGet_ReturnsSameCompany(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	got, err := s.GetCompany(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetCompany mismatch (-created +got):\n%s", diff)
	}
}

func TestGetCompany_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetCompany(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCompany error = %v, want ErrNotFound", err)
	}
}

func TestListActiveCompanies_ReturnsCreatedCompanies(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")

	got, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}

	want := []Company{acme, globex}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListActiveCompanies mismatch (-want +got):\n%s", diff)
	}
}

func TestSoftDeleteCompany_ExcludesFromListAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")

	if err := s.SoftDeleteCompany(ctx, acme.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}

	if _, err := s.GetCompany(ctx, acme.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCompany after delete error = %v, want ErrNotFound", err)
	}

	got, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	want := []Company{globex}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ListActiveCompanies mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateCompany_SameSourceRefAsSoftDeletedCompany_RestoresExistingRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	original := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if err := s.SoftDeleteCompany(ctx, original.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}

	readded, err := s.CreateCompany(ctx, "Acme Corp", "ashby", "acme")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}

	if readded.ID != original.ID {
		t.Fatalf("CreateCompany re-add ID = %d, want %d (same underlying row, not a new one)", readded.ID, original.ID)
	}
	if readded.Name != "Acme Corp" {
		t.Fatalf("CreateCompany re-add Name = %q, want %q", readded.Name, "Acme Corp")
	}
	if readded.DeletedAt != nil {
		t.Fatalf("CreateCompany re-add DeletedAt = %v, want nil (restored)", readded.DeletedAt)
	}

	list, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(list) != 1 || list[0].ID != original.ID {
		t.Fatalf("ListActiveCompanies after re-add = %+v, want [%d]", list, original.ID)
	}
}

func TestUpdateCompanyName_ChangesNameOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	updated, err := s.UpdateCompanyName(ctx, acme.ID, "Acme Corp")
	if err != nil {
		t.Fatalf("UpdateCompanyName: %v", err)
	}
	if updated.Name != "Acme Corp" {
		t.Fatalf("updated.Name = %q, want %q", updated.Name, "Acme Corp")
	}
	if updated.Source != "ashby" || updated.SourceRef != "acme" {
		t.Fatalf("updated.Source/SourceRef = %q/%q, want unchanged ashby/acme", updated.Source, updated.SourceRef)
	}

	got, err := s.GetCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if got.Name != "Acme Corp" {
		t.Fatalf("GetCompany after update Name = %q, want %q", got.Name, "Acme Corp")
	}
}

func TestUpdateCompanyName_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpdateCompanyName(ctx, 999, "New Name")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateCompanyName error = %v, want ErrNotFound", err)
	}
}

func TestUpdateCompanyName_SoftDeletedCompany_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if err := s.SoftDeleteCompany(ctx, acme.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}

	_, err := s.UpdateCompanyName(ctx, acme.ID, "New Name")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateCompanyName on soft-deleted company error = %v, want ErrNotFound", err)
	}
}

func TestRestoreCompany_UndoesSoftDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if err := s.SoftDeleteCompany(ctx, acme.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}

	if err := s.RestoreCompany(ctx, acme.ID); err != nil {
		t.Fatalf("RestoreCompany: %v", err)
	}

	got, err := s.GetCompany(ctx, acme.ID)
	if err != nil {
		t.Fatalf("GetCompany after restore: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatalf("expected DeletedAt nil after restore, got %v", got.DeletedAt)
	}

	list, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(list) != 1 || list[0].ID != acme.ID {
		t.Fatalf("ListActiveCompanies after restore = %+v, want [acme]", list)
	}
}
