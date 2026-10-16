package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type stubPeopleStore struct {
	people []*model.Person

	createErr, updateErr, deleteErr error
	created                         *model.Person
	updatedName, updatedColor       string
	updateCalls, deleteCalls        int
}

func (s *stubPeopleStore) GetAll(ctx context.Context) ([]*model.Person, error) {
	return s.people, nil
}

func (s *stubPeopleStore) Create(ctx context.Context, initial, name, color string) (*model.Person, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.created = &model.Person{ID: uuid.New(), Initial: initial, Name: name, Color: color}
	return s.created, nil
}

func (s *stubPeopleStore) Update(ctx context.Context, id uuid.UUID, name, color string) error {
	s.updateCalls++
	s.updatedName, s.updatedColor = name, color
	if s.updateErr == nil {
		for _, person := range s.people {
			if person.ID == id {
				person.Name, person.Color = name, color
			}
		}
	}
	return s.updateErr
}

func (s *stubPeopleStore) Delete(ctx context.Context, id uuid.UUID) error {
	s.deleteCalls++
	return s.deleteErr
}

func postPeople(h http.HandlerFunc, path, id string, form url.Values) *httptest.ResponseRecorder {
	return postPeopleWithHeaders(h, path, id, form, nil)
}

func postPeopleWithHeaders(h http.HandlerFunc, path, id string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	recorder := httptest.NewRecorder()
	h(recorder, req)
	return recorder
}

func TestCreatePerson(t *testing.T) {
	cases := []struct {
		name         string
		form         url.Values
		people       []*model.Person
		wantLocation string
		wantInitial  string
		wantName     string
		wantColor    string
	}{
		{"explicit fields", url.Values{"name": {" Alex "}, "initial": {"x"}, "color": {"mint"}}, nil, "/people", "X", "Alex", "mint"},
		{"initial defaults to first letter", url.Values{"name": {"élodie"}}, nil, "/people", "É", "élodie", "cyan"},
		{"colour defaults to an unused one", url.Values{"name": {"Blake"}}, []*model.Person{{Color: "cyan"}, {Color: "rose"}}, "/people", "B", "Blake", "amber"},
		{"blank name", url.Values{"name": {"  "}}, nil, "/people?error=invalid_name", "", "", ""},
		{"name too long", url.Values{"name": {strings.Repeat("a", 51)}}, nil, "/people?error=invalid_name", "", "", ""},
		{"control character in name", url.Values{"name": {"Al\x00ex"}}, nil, "/people?error=invalid_name", "", "", ""},
		{"multi-letter initial", url.Values{"name": {"Alex"}, "initial": {"AB"}}, nil, "/people?error=invalid_initial", "", "", ""},
		{"digit initial", url.Values{"name": {"Alex"}, "initial": {"7"}}, nil, "/people?error=invalid_initial", "", "", ""},
		{"name starting with a digit", url.Values{"name": {"7th Guest"}}, nil, "/people?error=invalid_initial", "", "", ""},
		{"unknown colour", url.Values{"name": {"Alex"}, "color": {"teal"}}, nil, "/people?error=invalid_color", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubPeopleStore{people: tc.people}
			recorder := postPeople((&PeopleHandler{personRepo: store}).Create, "/people", "", tc.form)
			// Rejections also carry the typed fields back, so compare the error only.
			location, err := url.Parse(recorder.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			wantError := strings.TrimPrefix(tc.wantLocation, "/people?error=")
			if tc.wantLocation == "/people" {
				wantError = ""
			}
			if recorder.Code != http.StatusSeeOther || location.Path != "/people" || location.Query().Get("error") != wantError {
				t.Fatalf("got %d %q, want 303 %q", recorder.Code, location, tc.wantLocation)
			}
			if tc.wantInitial == "" {
				if store.created != nil {
					t.Fatalf("rejected form still created %+v", store.created)
				}
				return
			}
			got := store.created
			if got == nil || got.Initial != tc.wantInitial || got.Name != tc.wantName || got.Color != tc.wantColor {
				t.Fatalf("created %+v, want initial=%q name=%q color=%q", got, tc.wantInitial, tc.wantName, tc.wantColor)
			}
		})
	}
}

func TestCreatePersonReportsTakenInitial(t *testing.T) {
	store := &stubPeopleStore{createErr: repository.ErrPersonInitialTaken}
	recorder := postPeople((&PeopleHandler{personRepo: store}).Create, "/people", "", url.Values{"name": {"Alex"}, "initial": {"a"}, "color": {"mint"}})
	if got, want := recorder.Header().Get("Location"), "/people?color=mint&error=initial_taken&initial=a&name=Alex"; got != want {
		t.Fatalf("location %q, want %q", got, want)
	}

	store.createErr = errors.New("boom")
	recorder = postPeople((&PeopleHandler{personRepo: store}).Create, "/people", "", url.Values{"name": {"Alex"}})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected error gave status %d", recorder.Code)
	}
}

func TestUpdatePerson(t *testing.T) {
	id := uuid.NewString()
	cases := []struct {
		name         string
		id           string
		form         url.Values
		updateErr    error
		wantCode     int
		wantLocation string
		wantCalls    int
	}{
		{"renames and recolours", id, url.Values{"name": {" Dan "}, "color": {"rose"}}, nil, http.StatusSeeOther, "/people", 1},
		{"invalid id", "nope", url.Values{"name": {"Dan"}, "color": {"rose"}}, nil, http.StatusBadRequest, "", 0},
		{"blank name", id, url.Values{"name": {""}, "color": {"rose"}}, nil, http.StatusSeeOther, "/people?error=invalid_name", 0},
		{"missing colour", id, url.Values{"name": {"Dan"}}, nil, http.StatusSeeOther, "/people?error=invalid_color", 0},
		{"unknown person", id, url.Values{"name": {"Dan"}, "color": {"rose"}}, repository.ErrPersonNotFound, http.StatusSeeOther, "/people?error=not_found", 1},
		{"unexpected error", id, url.Values{"name": {"Dan"}, "color": {"rose"}}, errors.New("boom"), http.StatusInternalServerError, "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubPeopleStore{updateErr: tc.updateErr}
			recorder := postPeople((&PeopleHandler{personRepo: store}).Update, "/people/"+tc.id, tc.id, tc.form)
			if recorder.Code != tc.wantCode || recorder.Header().Get("Location") != tc.wantLocation {
				t.Fatalf("got %d %q, want %d %q", recorder.Code, recorder.Header().Get("Location"), tc.wantCode, tc.wantLocation)
			}
			if store.updateCalls != tc.wantCalls {
				t.Fatalf("update calls = %d, want %d", store.updateCalls, tc.wantCalls)
			}
			if tc.name == "renames and recolours" && (store.updatedName != "Dan" || store.updatedColor != "rose") {
				t.Fatalf("stored %q/%q", store.updatedName, store.updatedColor)
			}
		})
	}
}

// TestUpdatePersonInPlace covers a Save sent by htmx from a person's card:
// the response is just that card, never a redirect that reloads the page.
func TestUpdatePersonInPlace(t *testing.T) {
	htmx := map[string]string{"HX-Request": "true"}
	newStore := func() (*stubPeopleStore, *model.Person) {
		dan := &model.Person{ID: uuid.New(), Initial: "D", Name: "Dan", Color: "cyan"}
		return &stubPeopleStore{people: []*model.Person{dan}}, dan
	}

	t.Run("saves and returns the card", func(t *testing.T) {
		store, dan := newStore()
		id := dan.ID.String()
		recorder := postPeopleWithHeaders((&PeopleHandler{personRepo: store}).Update, "/people/"+id, id, url.Values{"name": {"Daniel"}, "color": {"rose"}}, htmx)
		body := recorder.Body.String()
		if recorder.Code != http.StatusOK || recorder.Header().Get("Location") != "" {
			t.Fatalf("got %d location %q, want 200 with no redirect", recorder.Code, recorder.Header().Get("Location"))
		}
		if !strings.Contains(recorder.Header().Get("HX-Trigger"), "Saved!") {
			t.Errorf("no saved toast: %q", recorder.Header().Get("HX-Trigger"))
		}
		for _, want := range []string{
			`<div id="person-` + id + `" class="card person-rose">`,
			`hx-target="#person-` + id + `"`,
			`value="Daniel"`,
			`value="rose" checked`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("card is missing %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "<html") || strings.Contains(body, `role="alert"`) {
			t.Errorf("in-place save returned more than a clean card:\n%s", body)
		}
	})

	t.Run("rejected save keeps what was typed", func(t *testing.T) {
		store, dan := newStore()
		id := dan.ID.String()
		recorder := postPeopleWithHeaders((&PeopleHandler{personRepo: store}).Update, "/people/"+id, id, url.Values{"name": {"Daniel"}, "color": {"teal"}}, htmx)
		body := recorder.Body.String()
		if recorder.Code != http.StatusOK || store.updateCalls != 0 || recorder.Header().Get("HX-Trigger") != "" {
			t.Fatalf("got %d, %d updates, trigger %q; want 200, no update, no toast", recorder.Code, store.updateCalls, recorder.Header().Get("HX-Trigger"))
		}
		for _, want := range []string{`role="alert">Pick one of the listed colours.`, `value="Daniel"`, `class="card person-cyan"`} {
			if !strings.Contains(body, want) {
				t.Errorf("card is missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("unknown person reloads the page with a message", func(t *testing.T) {
		store, _ := newStore()
		id := uuid.NewString()
		store.updateErr = repository.ErrPersonNotFound
		recorder := postPeopleWithHeaders((&PeopleHandler{personRepo: store}).Update, "/people/"+id, id, url.Values{"name": {"Dan"}, "color": {"rose"}}, htmx)
		if got := recorder.Header().Get("HX-Redirect"); got != "/people?error=not_found" {
			t.Fatalf("HX-Redirect = %q", got)
		}
	})

	t.Run("boosted post still redirects", func(t *testing.T) {
		store, dan := newStore()
		id := dan.ID.String()
		recorder := postPeopleWithHeaders((&PeopleHandler{personRepo: store}).Update, "/people/"+id, id, url.Values{"name": {"Dan"}, "color": {"rose"}},
			map[string]string{"HX-Request": "true", "HX-Boosted": "true"})
		if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/people" {
			t.Fatalf("got %d %q, want 303 /people", recorder.Code, recorder.Header().Get("Location"))
		}
	})
}

func TestDeletePerson(t *testing.T) {
	id := uuid.NewString()
	cases := []struct {
		name         string
		id           string
		deleteErr    error
		wantCode     int
		wantLocation string
	}{
		{"removes", id, nil, http.StatusSeeOther, "/people"},
		{"invalid id", "nope", nil, http.StatusBadRequest, ""},
		{"has ratings or picks", id, repository.ErrPersonInUse, http.StatusSeeOther, "/people?error=in_use"},
		{"unknown person", id, repository.ErrPersonNotFound, http.StatusSeeOther, "/people?error=not_found"},
		{"unexpected error", id, errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubPeopleStore{deleteErr: tc.deleteErr}
			recorder := postPeople((&PeopleHandler{personRepo: store}).Delete, "/people/"+tc.id+"/delete", tc.id, nil)
			if recorder.Code != tc.wantCode || recorder.Header().Get("Location") != tc.wantLocation {
				t.Fatalf("got %d %q, want %d %q", recorder.Code, recorder.Header().Get("Location"), tc.wantCode, tc.wantLocation)
			}
		})
	}
}

func TestPeoplePageShowsPeopleAndErrors(t *testing.T) {
	alex := &model.Person{ID: uuid.New(), Initial: "A", Name: "Alex", Color: "rose"}
	store := &stubPeopleStore{people: []*model.Person{alex}}
	req := httptest.NewRequest(http.MethodGet, "/people?error=in_use", nil)
	recorder := httptest.NewRecorder()
	(&PeopleHandler{personRepo: store}).PeoplePage(recorder, req)

	body := recorder.Body.String()
	removeForm := "remove-" + alex.ID.String()
	for _, want := range []string{
		"Alex", "person-rose", "Can&#39;t remove someone who has ratings or picks", "Add a person",
		// The help text states the Trophy Room rule and today's numbers.
		"once all but one person has rated it", "1 of 1 people.",
		`<form id="` + removeForm + `" action="/people/` + alex.ID.String() + `/delete" method="POST"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q:\n%s", want, body)
		}
	}

	// Save and Remove share one action row; Remove submits the delete form by id.
	_, actions, _ := strings.Cut(body, `class="people-actions people-card-actions"`)
	actions, _, _ = strings.Cut(actions, "</div>")
	for _, want := range []string{">Save</button>", `form="` + removeForm + `" class="btn-danger">Remove</button>`} {
		if !strings.Contains(actions, want) {
			t.Errorf("person action row is missing %q: %s", want, actions)
		}
	}

	// With four people, three ratings are enough.
	store.people = []*model.Person{alex, {ID: uuid.New()}, {ID: uuid.New()}, {ID: uuid.New()}}
	recorder = httptest.NewRecorder()
	(&PeopleHandler{personRepo: store}).PeoplePage(recorder, httptest.NewRequest(http.MethodGet, "/people", nil))
	if !strings.Contains(recorder.Body.String(), "3 of 4 people.") {
		t.Errorf("help text does not say 3 of 4:\n%s", recorder.Body.String())
	}
	store.people = []*model.Person{alex}

	// A rejected add form keeps what was typed.
	req = httptest.NewRequest(http.MethodGet, "/people?error=initial_taken&name=Zo%C3%AB&initial=Z&color=mint", nil)
	recorder = httptest.NewRecorder()
	(&PeopleHandler{personRepo: store}).PeoplePage(recorder, req)
	for _, want := range []string{`value="Zoë"`, `value="Z"`, `value="mint" checked`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("rejected form lost %q:\n%s", want, recorder.Body.String())
		}
	}

	// Unknown codes never echo into the page.
	req = httptest.NewRequest(http.MethodGet, "/people?error=%3Cscript%3E", nil)
	recorder = httptest.NewRecorder()
	(&PeopleHandler{personRepo: store}).PeoplePage(recorder, req)
	if strings.Contains(recorder.Body.String(), "role=\"alert\"") {
		t.Error("unknown error code rendered an alert")
	}
}

func TestSuggestColor(t *testing.T) {
	if got := suggestColor(nil); got != "cyan" {
		t.Errorf("empty = %q", got)
	}
	all := make([]*model.Person, len(model.PersonColors))
	for i, color := range model.PersonColors {
		all[i] = &model.Person{Color: color}
	}
	if got := suggestColor(all); got != model.PersonColors[0] {
		t.Errorf("all used = %q", got)
	}
}
