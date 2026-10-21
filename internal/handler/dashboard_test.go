package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/session"
	"github.com/google/uuid"
)

type stubDashboardEntries struct {
	entries []*model.Entry
	state   model.EntryGroupState
	err     error
}

func (s *stubDashboardEntries) ListDashboard(ctx context.Context) ([]*model.Entry, model.EntryGroupState, error) {
	return s.entries, s.state, s.err
}

func dashboardEntry(group int, title string) *model.Entry {
	return &model.Entry{ID: uuid.New(), GroupNumber: group, Movie: &model.Movie{Title: title}}
}

func TestGroupEntriesKeepsListOrder(t *testing.T) {
	entries := []*model.Entry{
		dashboardEntry(4, "Fargo"), dashboardEntry(4, "Heat"),
		dashboardEntry(2, "Dune"),
		dashboardEntry(1, "Alien"), dashboardEntry(1, "Brazil"), dashboardEntry(1, "Coco"),
	}
	groups := groupEntries(entries)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	wantSizes := map[int]int{4: 2, 2: 1, 1: 3}
	next := 0
	for i, group := range groups {
		if i > 0 && groups[i-1].Number <= group.Number {
			t.Fatalf("group %d listed after group %d", group.Number, groups[i-1].Number)
		}
		if len(group.Entries) != wantSizes[group.Number] {
			t.Fatalf("group %d has %d entries, want %d", group.Number, len(group.Entries), wantSizes[group.Number])
		}
		for _, entry := range group.Entries {
			if entry != entries[next] {
				t.Fatalf("group %d entry %q out of order", group.Number, entry.Movie.Title)
			}
			next++
		}
	}
	if groupEntries(nil) != nil {
		t.Fatal("expected no groups for no entries")
	}
}

func TestDashboardCurrentGroup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []*model.Entry
		want    string
	}{
		{"empty", nil, `<option value="1" selected>Group 1 (new)</option>`},
		{"highest group", []*model.Entry{dashboardEntry(3, "Cube"), dashboardEntry(1, "Alien")}, `<option value="4">+ New group</option>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &DashboardHandler{entryRepo: &stubDashboardEntries{entries: tc.entries}, personRepo: &stubPersonRepo{}}
			manager := session.NewManager("secret", time.Hour, false, 0)
			serve := middleware.Auth(manager)(http.HandlerFunc(handler.DashboardContent))

			// The group picker is part of the Add movie panel, which only
			// logged-in people see.
			req := httptest.NewRequest(http.MethodGet, "/dashboard-content", nil)
			cookie, err := manager.NewCookie()
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			serve.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), tc.want) {
				t.Fatalf("status %d, body missing %q:\n%s", recorder.Code, tc.want, recorder.Body.String())
			}

			recorder = httptest.NewRecorder()
			serve.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard-content", nil))
			if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "add-group-select") {
				t.Fatalf("logged out: status %d, body has the Add movie panel:\n%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestDashboardNeedsRatingFilter(t *testing.T) {
	dan := &model.Person{ID: uuid.New(), Initial: "D", Name: "Dan", Color: "cyan"}
	jen := &model.Person{ID: uuid.New(), Initial: "J", Name: "Jen", Color: "rose"}
	cal := &model.Person{ID: uuid.New(), Initial: "C", Name: "Caleb", Color: "amber"}
	rated := func(entry *model.Entry, persons ...*model.Person) *model.Entry {
		for _, p := range persons {
			entry.Ratings = append(entry.Ratings, &model.Rating{PersonID: p.ID, Person: p, Score: 7})
		}
		return entry
	}
	entries := []*model.Entry{
		rated(dashboardEntry(2, "Heat"), dan, cal),
		rated(dashboardEntry(2, "Alien"), cal),
		rated(dashboardEntry(1, "Coco"), dan, jen, cal),
	}
	handler := &DashboardHandler{
		entryRepo:  &stubDashboardEntries{entries: entries},
		personRepo: &stubPersonRepo{persons: []*model.Person{cal, dan, jen}},
	}

	get := func(target string, cookies ...*http.Cookie) (string, *http.Cookie) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		recorder := httptest.NewRecorder()
		handler.DashboardContent(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d", target, recorder.Code)
		}
		var me *http.Cookie
		for _, c := range recorder.Result().Cookies() {
			if c.Name == meCookie {
				me = c
			}
		}
		return recorder.Body.String(), me
	}
	shows := func(t *testing.T, body string, want ...string) {
		t.Helper()
		for _, title := range []string{"Heat", "Alien", "Coco"} {
			wanted := false
			for _, w := range want {
				wanted = wanted || w == title
			}
			if got := strings.Contains(body, title); got != wanted {
				t.Errorf("shows %s = %v, want %v", title, got, wanted)
			}
		}
	}

	t.Run("unfiltered", func(t *testing.T) {
		body, me := get("/dashboard-content")
		shows(t, body, "Heat", "Alien", "Coco")
		if me != nil {
			t.Errorf("set %s without a needs parameter", meCookie)
		}
		// Caleb has rated everything, so only Dan (1) and Jen (2) wait.
		for _, want := range []string{`href="/?needs=D"`, `<span class="nudge-count">1</span>`, `href="/?needs=J"`, `<span class="nudge-count">2</span>`} {
			if !strings.Contains(body, want) {
				t.Errorf("waiting line is missing %q", want)
			}
		}
		if strings.Contains(body, `href="/?needs=C"`) {
			t.Error("waiting line lists Caleb, who rated every movie")
		}
		// Posters say who has not rated.
		if !strings.Contains(body, `title="Waiting on Jen"`) {
			t.Error("posters do not show who is missing")
		}
	})

	t.Run("needs sets the cookie", func(t *testing.T) {
		body, me := get("/dashboard-content?needs=j")
		shows(t, body, "Heat", "Alien")
		if me == nil || me.Value != "J" || me.MaxAge <= 0 || !me.HttpOnly {
			t.Fatalf("%s cookie = %+v, want a lasting J", meCookie, me)
		}
		if !strings.Contains(body, `<option value="J" selected>Jen</option>`) {
			t.Error("filter does not select Jen")
		}
	})

	t.Run("cookie is the default", func(t *testing.T) {
		body, me := get("/dashboard-content", &http.Cookie{Name: meCookie, Value: "D"})
		shows(t, body, "Alien")
		if me != nil {
			t.Errorf("rewrote %s without a needs parameter", meCookie)
		}
	})

	t.Run("caught up", func(t *testing.T) {
		body, _ := get("/dashboard-content?needs=C")
		shows(t, body)
		if !strings.Contains(body, "Caleb has rated every movie.") {
			t.Error("missing the caught-up note")
		}
	})

	for _, target := range []string{"/dashboard-content?needs=", "/dashboard-content?needs=Z"} {
		t.Run("clears with "+target, func(t *testing.T) {
			body, me := get(target, &http.Cookie{Name: meCookie, Value: "D"})
			shows(t, body, "Heat", "Alien", "Coco")
			if me == nil || me.MaxAge >= 0 {
				t.Fatalf("%s cookie = %+v, want it cleared", meCookie, me)
			}
			if !strings.Contains(body, `<option value="" selected>Anyone</option>`) {
				t.Error("filter does not fall back to Anyone")
			}
		})
	}
}

func TestDashboardReturns500WhenEntriesFail(t *testing.T) {
	handler := &DashboardHandler{entryRepo: &stubDashboardEntries{err: errors.New("db down")}, personRepo: &stubPersonRepo{}}
	for path, serve := range map[string]http.HandlerFunc{
		"/":                  handler.DashboardPage,
		"/dashboard-content": handler.DashboardContent,
	} {
		recorder := httptest.NewRecorder()
		serve(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("%s: status %d, want 500", path, recorder.Code)
		}
		if strings.Contains(recorder.Body.String(), "group-section") {
			t.Fatalf("%s: rendered groups despite the error", path)
		}
	}
}

func TestDashboardCookiePrivacy(t *testing.T) {
	person := &model.Person{ID: uuid.New(), Initial: "A", Name: "Viewer", Color: "cyan"}
	for _, secure := range []bool{false, true} {
		h := &DashboardHandler{entryRepo: &stubDashboardEntries{}, personRepo: &stubPersonRepo{persons: []*model.Person{person}}, secureCookies: secure}
		for path, serve := range map[string]http.HandlerFunc{"/": h.DashboardPage, "/dashboard-content": h.DashboardContent} {
			for _, tc := range []struct{ name, query, cookie, want string }{
				{"absent", "", "", "none"},
				{"set", "?needs=A", "", "set"},
				{"read", "", "A", "none"},
				{"clear query", "?needs=", "A", "clear"},
				{"invalid escape", "", "%ZZ", "clear"},
				{"invalid syntax", "", "\"unterminated", "clear"},
				{"unknown or deleted person", "", "Z", "clear"},
				{"blank", "", "%20", "clear"},
			} {
				t.Run(path+tc.name+map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
					req := httptest.NewRequest(http.MethodGet, path+tc.query, nil)
					if tc.cookie != "" {
						req.Header.Set("Cookie", meCookie+"="+tc.cookie)
					}
					rec := httptest.NewRecorder()
					rec.Header().Add("Vary", "Accept-Encoding")
					rec.Header().Set("X-Preserved", "yes")
					serve(rec, req)
					if rec.Header().Get("Cache-Control") != "private, no-store" {
						t.Fatal("missing private cache policy")
					}
					vary := strings.Join(rec.Header().Values("Vary"), ",")
					if !strings.Contains(vary, "Cookie") || !strings.Contains(vary, "Accept-Encoding") || rec.Header().Get("X-Preserved") != "yes" {
						t.Fatalf("headers not preserved: %v", rec.Header())
					}
					cookies := rec.Result().Cookies()
					if tc.want == "none" {
						if len(cookies) != 0 {
							t.Fatalf("unexpected cookie: %v", cookies)
						}
						return
					}
					if len(cookies) != 1 {
						t.Fatalf("cookies = %v", cookies)
					}
					c := cookies[0]
					if c.Name != meCookie || c.Path != "/" || !c.HttpOnly || c.Secure != secure || c.SameSite != http.SameSiteLaxMode {
						t.Fatalf("cookie flags: %+v", c)
					}
					if tc.want == "clear" && (c.Value != "" || c.MaxAge != -1) {
						t.Fatalf("not cleared: %+v", c)
					}
					if tc.want == "set" && (c.Value != "A" || c.MaxAge != meCookieMaxAge) {
						t.Fatalf("not set: %+v", c)
					}
				})
			}
		}
	}
}

func TestDashboardRendersPersistedGroupState(t *testing.T) {
	entries := make([]*model.Entry, 9)
	for i := range entries {
		entries[i] = dashboardEntry(1, "Compacted normal pick")
	}
	h := &DashboardHandler{entryRepo: &stubDashboardEntries{entries: entries, state: model.EntryGroupState{Version: 7, FirstGroupCapacity: 9}}, personRepo: &stubPersonRepo{}}
	manager := session.NewManager("fixture-only", time.Hour, false, 0)
	cookie, err := manager.NewCookie()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/dashboard-content"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		view := h.DashboardContent
		if path == "/" {
			view = h.DashboardPage
		}
		middleware.Auth(manager)(http.HandlerFunc(view)).ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "9 out of 9") || strings.Contains(body, "9 out of 8") || !strings.Contains(body, `data-group-version="7"`) {
			t.Fatalf("%s: status=%d missing persisted state: %s", path, rec.Code, body)
		}
	}
}
