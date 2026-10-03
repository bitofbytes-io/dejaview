package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type stubRatingRepo struct {
	deleteCalls int
	upsertCalls int
}

func (s *stubRatingRepo) SaveBatch(ctx context.Context, entryID uuid.UUID, changes []model.RatingChange) error {
	for _, change := range changes {
		if change.Score == nil {
			s.deleteCalls++
		} else {
			s.upsertCalls++
		}
	}
	return nil
}

type stubEntryRepo struct {
	entries []*model.Entry
	errs    []error
	calls   int
}

func (s *stubEntryRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Entry, error) {
	defer func() { s.calls++ }()
	if s.calls < len(s.entries) {
		var err error
		if s.calls < len(s.errs) {
			err = s.errs[s.calls]
		}
		return s.entries[s.calls], err
	}
	return nil, nil
}

type stubPersonRepo struct {
	calls int
}

func (s *stubPersonRepo) GetAll(ctx context.Context) ([]*model.Person, error) {
	s.calls++
	return nil, nil
}

func TestSaveRatings_EntryMissingAfterRefetch(t *testing.T) {
	entryID := uuid.New()
	ratingRepo := &stubRatingRepo{}
	entryRepo := &stubEntryRepo{
		entries: []*model.Entry{{ID: entryID}},
		errs:    []error{nil, nil},
	}
	personRepo := &stubPersonRepo{}

	handler := &RatingHandler{
		ratingRepo: ratingRepo,
		entryRepo:  entryRepo,
		personRepo: personRepo,
	}

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/entries/"+entryID.String()+"/ratings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", entryID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()

	handler.SaveRatings(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	if entryRepo.calls != 2 {
		t.Fatalf("expected entry repo to be called twice, got %d", entryRepo.calls)
	}
	if personRepo.calls != 0 {
		t.Fatalf("expected persons fetch to be skipped, got %d", personRepo.calls)
	}
	if ratingRepo.upsertCalls != 0 || ratingRepo.deleteCalls != 0 {
		t.Fatalf("expected no rating repo mutations, got upserts=%d deletes=%d", ratingRepo.upsertCalls, ratingRepo.deleteCalls)
	}
}

func TestSaveRatings_InvalidEntryID(t *testing.T) {
	ratingRepo := &stubRatingRepo{}
	entryRepo := &stubEntryRepo{}
	personRepo := &stubPersonRepo{}

	handler := &RatingHandler{
		ratingRepo: ratingRepo,
		entryRepo:  entryRepo,
		personRepo: personRepo,
	}

	req := httptest.NewRequest(http.MethodPost, "/entries/not-a-uuid/ratings", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", "not-a-uuid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()

	handler.SaveRatings(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
	if entryRepo.calls != 0 {
		t.Fatalf("expected entry repo not to be called, got %d", entryRepo.calls)
	}
	if personRepo.calls != 0 {
		t.Fatalf("expected persons fetch not to be called, got %d", personRepo.calls)
	}
	if ratingRepo.upsertCalls != 0 || ratingRepo.deleteCalls != 0 {
		t.Fatalf("expected no rating repo mutations, got upserts=%d deletes=%d", ratingRepo.upsertCalls, ratingRepo.deleteCalls)
	}
}

func TestSaveRatings_InvalidForm(t *testing.T) {
	entryID := uuid.New()
	ratingRepo := &stubRatingRepo{}
	entryRepo := &stubEntryRepo{}
	personRepo := &stubPersonRepo{}

	handler := &RatingHandler{
		ratingRepo: ratingRepo,
		entryRepo:  entryRepo,
		personRepo: personRepo,
	}

	req := httptest.NewRequest(http.MethodPost, "/entries/"+entryID.String()+"/ratings", strings.NewReader("rating%ZZ"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", entryID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()

	handler.SaveRatings(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
	if entryRepo.calls != 0 {
		t.Fatalf("expected entry repo not to be called, got %d", entryRepo.calls)
	}
	if personRepo.calls != 0 {
		t.Fatalf("expected persons fetch not to be called, got %d", personRepo.calls)
	}
	if ratingRepo.upsertCalls != 0 || ratingRepo.deleteCalls != 0 {
		t.Fatalf("expected no rating repo mutations, got upserts=%d deletes=%d", ratingRepo.upsertCalls, ratingRepo.deleteCalls)
	}
}

func TestSaveRatings_UpsertAndDelete(t *testing.T) {
	entryID := uuid.New()
	existingPersonID := uuid.New()
	validPersonID := uuid.New()

	ratingRepo := &stubRatingRepo{}
	entryRepo := &stubEntryRepo{
		entries: []*model.Entry{
			{
				ID: entryID,
				Ratings: []*model.Rating{
					{PersonID: existingPersonID},
				},
			},
			{
				ID: entryID,
			},
		},
		errs: []error{nil, nil},
	}
	personRepo := &stubPersonRepo{}

	handler := &RatingHandler{
		ratingRepo: ratingRepo,
		entryRepo:  entryRepo,
		personRepo: personRepo,
	}

	form := url.Values{}
	form.Set("rating["+existingPersonID.String()+"]", "")
	form.Set("rating["+validPersonID.String()+"]", "8.5")

	req := httptest.NewRequest(http.MethodPost, "/entries/"+entryID.String()+"/ratings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", entryID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	recorder := httptest.NewRecorder()

	handler.SaveRatings(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	// The response replaces #ratings-section via outerHTML, so it must keep that id.
	if !strings.HasPrefix(strings.TrimSpace(recorder.Body.String()), `<div class="card" id="ratings-section">`) {
		t.Fatalf("expected ratings card fragment root, got %q", recorder.Body.String())
	}
	if entryRepo.calls != 2 {
		t.Fatalf("expected entry repo to be called twice, got %d", entryRepo.calls)
	}
	if personRepo.calls != 1 {
		t.Fatalf("expected persons fetch to be called once, got %d", personRepo.calls)
	}
	if ratingRepo.deleteCalls != 1 {
		t.Fatalf("expected one delete call, got %d", ratingRepo.deleteCalls)
	}
	if ratingRepo.upsertCalls != 1 {
		t.Fatalf("expected one upsert call, got %d", ratingRepo.upsertCalls)
	}
}

func TestSaveRatings_InvalidBatchDoesNotWrite(t *testing.T) {
	personID := uuid.NewString()
	valid := "rating[" + uuid.NewString() + "]=8"
	cases := []string{
		"rating[not-a-uuid]=7", "rating[" + personID + "]=NaN", "rating[" + personID + "]=Inf",
		"rating[" + personID + "]=-1", "rating[" + personID + "]=11", "rating[" + personID + "]=abc",
		"rating[" + personID + "]=7&rating[" + personID + "]=8", "rating[" + personID + "=7",
		"rating[" + personID + "]=7&rating[" + strings.ToUpper(personID) + "]=8",
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			entryID := uuid.New()
			ratings := &stubRatingRepo{}
			handler := &RatingHandler{ratingRepo: ratings, entryRepo: &stubEntryRepo{entries: []*model.Entry{{ID: entryID}}}}
			req := httptest.NewRequest(http.MethodPost, "/entries/"+entryID.String()+"/ratings", strings.NewReader(valid+"&"+body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			route := chi.NewRouteContext()
			route.URLParams.Add("id", entryID.String())
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
			result := httptest.NewRecorder()
			handler.SaveRatings(result, req)
			if result.Code != http.StatusBadRequest || ratings.upsertCalls != 0 || ratings.deleteCalls != 0 {
				t.Fatalf("status=%d upserts=%d deletes=%d", result.Code, ratings.upsertCalls, ratings.deleteCalls)
			}
		})
	}
}

func TestParseRatingChangesBoundaries(t *testing.T) {
	first, second := uuid.NewString(), uuid.NewString()
	changes, err := parseRatingChanges(url.Values{"rating[" + first + "]": {"0"}, "rating[" + second + "]": {"10"}})
	if err != nil || len(changes) != 2 {
		t.Fatalf("changes=%v err=%v", changes, err)
	}
}
