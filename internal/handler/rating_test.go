package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/metrics"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type stubRatingRepo struct {
	beforeSave  func()
	deleteCalls int
	upsertCalls int
	err         error
}

func (s *stubRatingRepo) SaveBatch(ctx context.Context, entryID uuid.UUID, changes []model.RatingChange) error {
	if s.beforeSave != nil {
		s.beforeSave()
	}
	if s.err != nil {
		return s.err
	}
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
	calls   int
	persons []*model.Person
}

func (s *stubPersonRepo) GetAll(ctx context.Context) ([]*model.Person, error) {
	s.calls++
	return s.persons, nil
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

func TestSaveRatings_MapsRepositoryErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unknown person", repository.ErrPersonNotFound, http.StatusBadRequest},
		{"entry deleted", repository.ErrEntryNotFound, http.StatusNotFound},
		{"unexpected", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entryID := uuid.New()
			handler := &RatingHandler{
				ratingRepo: &stubRatingRepo{err: tc.err},
				entryRepo:  &stubEntryRepo{entries: []*model.Entry{{ID: entryID}}},
				personRepo: &stubPersonRepo{},
			}
			body := "rating[" + uuid.NewString() + "]=8"
			req := httptest.NewRequest(http.MethodPost, "/entries/"+entryID.String()+"/ratings", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			route := chi.NewRouteContext()
			route.URLParams.Add("id", entryID.String())
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
			result := httptest.NewRecorder()
			handler.SaveRatings(result, req)
			if result.Code != tc.want {
				t.Fatalf("expected status %d, got %d", tc.want, result.Code)
			}
			if result.Header().Get("HX-Trigger") != "" {
				t.Fatal("expected no success toast")
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

// TestRatingBatchMetrics checks score counts at the atomic batch boundary,
// preserving no-op and cancellation behavior for every write outcome.
func TestRatingBatchMetrics(t *testing.T) {
	first, second, deleted := uuid.NewString(), uuid.NewString(), uuid.NewString()
	mixed := url.Values{"rating[" + first + "]": {"0"}, "rating[" + second + "]": {"10"}, "rating[" + deleted + "]": {""}}
	for _, tc := range []struct {
		name                      string
		form                      url.Values
		saveErr, errorAfterCommit error
		wantStatus                int
		want                      map[string]float64
	}{
		{name: "committed mixed batch", form: mixed, wantStatus: 200, want: map[string]float64{`{result="success"}`: 2}},
		{name: "unknown commit after cancellation", form: mixed, saveErr: errors.Join(repository.ErrCommitOutcomeUnknown, context.Canceled), wantStatus: 200, want: map[string]float64{`{result="unknown"}`: 2}},
		{name: "unknown commit after lost connection", form: mixed, saveErr: errors.Join(repository.ErrCommitOutcomeUnknown, errors.New("connection lost")), wantStatus: 500, want: map[string]float64{`{result="unknown"}`: 2}},
		{name: "rolled back batch", form: mixed, saveErr: errors.New("write rejected"), wantStatus: 500, want: map[string]float64{`{result="failure"}`: 2}},
		{name: "unknown person", form: mixed, saveErr: repository.ErrPersonNotFound, wantStatus: 400, want: map[string]float64{`{result="failure"}`: 2}},
		{name: "entry deleted", form: mixed, saveErr: repository.ErrEntryNotFound, wantStatus: 404, want: map[string]float64{`{result="failure"}`: 2}},
		{name: "canceled batch", form: mixed, saveErr: context.Canceled, wantStatus: 200, want: map[string]float64{`{result="failure"}`: 2}},
		{name: "refetch failure after commit", form: mixed, errorAfterCommit: errors.New("read failed"), wantStatus: 500, want: map[string]float64{`{result="success"}`: 2}},
		{name: "deletion only", form: url.Values{"rating[" + deleted + "]": {""}}, wantStatus: 200, want: map[string]float64{}},
		{name: "unknown deletion only", form: url.Values{"rating[" + deleted + "]": {""}}, saveErr: errors.Join(repository.ErrCommitOutcomeUnknown, context.Canceled), wantStatus: 200, want: map[string]float64{}},
		{name: "failed deletion only", form: url.Values{"rating[" + deleted + "]": {""}}, saveErr: errors.New("db down"), wantStatus: 500, want: map[string]float64{}},
		{name: "empty batch", form: url.Values{}, wantStatus: 200, want: map[string]float64{}},
		{name: "omitted ratings", form: url.Values{"other": {"value"}}, wantStatus: 200, want: map[string]float64{}},
		{name: "invalid score", form: url.Values{"rating[" + first + "]": {"11"}}, wantStatus: 400, want: map[string]float64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := metrics.New()
			id := uuid.New()
			ratings := &stubRatingRepo{err: tc.saveErr, beforeSave: func() { assertDomainMetrics(t, m, "dejaview_ratings_saved_total", map[string]float64{}) }}
			entries := &stubEntryRepo{entries: []*model.Entry{{ID: id}, {ID: id}}, errs: []error{nil, tc.errorAfterCommit}}
			h := &RatingHandler{ratingRepo: ratings, entryRepo: entries, personRepo: &stubPersonRepo{}, metrics: m}
			assertDomainMetrics(t, m, "dejaview_ratings_saved_total", map[string]float64{})
			req := httptest.NewRequest(http.MethodPut, "/api/entries/"+id.String()+"/ratings", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			route := chi.NewRouteContext()
			route.URLParams.Add("id", id.String())
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
			rec := httptest.NewRecorder()
			h.SaveRatings(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			assertDomainMetrics(t, m, "dejaview_ratings_saved_total", tc.want)
			if tc.saveErr != nil && (ratings.upsertCalls != 0 || ratings.deleteCalls != 0) {
				t.Fatal("failed batch mutated ratings")
			}
		})
	}
}
